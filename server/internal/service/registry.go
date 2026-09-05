// Package service 实现 Server 业务层: 注册、数据校验、实时监控管理。
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// 注册业务错误 → API 层映射为 HTTP 状态码。
var (
	ErrCodeInvalid         = errors.New("registry: register code invalid")      // 401
	ErrCodeExpired         = errors.New("registry: register code expired")      // 401
	ErrCodeUsed            = errors.New("registry: register code already used") // 401
	ErrTooManyCodes        = errors.New("registry: too many active codes")      // 429
	ErrFingerprintConflict = errors.New("registry: fingerprint conflict")       // 409
	ErrInvalidRegisterReq  = errors.New("registry: invalid register request")   // 400
)

// codeAlphabet 注册码字符集: 去除易混淆字符(I/L/O/0/1)。
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// Registry 注册码签发与 Agent 注册(设计文档 4.2)。
type Registry struct {
	agents  *repository.AgentRepo
	codes   *repository.RegisterCodeRepo
	codeTTL time.Duration // 默认 15 分钟
	now     func() time.Time
}

func NewRegistry(agents *repository.AgentRepo, codes *repository.RegisterCodeRepo) *Registry {
	return &Registry{agents: agents, codes: codes, codeTTL: 15 * time.Minute, now: time.Now}
}

// SetCodeTTL 覆盖注册码有效期(测试用)。
func (s *Registry) SetCodeTTL(d time.Duration) { s.codeTTL = d }

// IssueCodeForNode 为预创建节点生成绑定注册码(Komari 模式)。
func (s *Registry) IssueCodeForNode(ctx context.Context, agentID int64) (string, time.Time, error) {
	n, err := s.codes.CountActive(ctx, s.now().Unix())
	if err != nil {
		return "", time.Time{}, err
	}
	if n >= 5 {
		return "", time.Time{}, ErrTooManyCodes
	}
	code, err := randomCode(10)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.now().Add(s.codeTTL)
	if err := s.codes.CreateBind(ctx, pkg.SHA256Hex(code), expires.Unix(), agentID); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// IssueCode 生成一次性注册码: 随机 10 字符, 有效期 15 分钟, 未使用上限 5 个。
// 返回注册码原文(仅此一次, 服务端只存哈希)。
func (s *Registry) IssueCode(ctx context.Context) (string, time.Time, error) {
	n, err := s.codes.CountActive(ctx, s.now().Unix())
	if err != nil {
		return "", time.Time{}, err
	}
	if n >= 5 {
		return "", time.Time{}, ErrTooManyCodes
	}
	code, err := randomCode(10)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.now().Add(s.codeTTL)
	if err := s.codes.Create(ctx, pkg.SHA256Hex(code), expires.Unix()); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// Register 用注册码换取持久 Token(设计文档 4.2 六步流程)。
// 流程: 校验请求 → 校验注册码 → 指纹冲突检查 → 建 Agent → 一次性消费注册码。
// Token 仅在响应中出现一次, 服务端只存 SHA256(S9)。
func (s *Registry) Register(ctx context.Context, req *model.RegisterRequest, remoteIP string) (*model.RegisterResponse, error) {
	if req == nil || req.RegisterCode == "" || req.Hostname == "" || req.HostFingerprint == "" {
		return nil, ErrInvalidRegisterReq
	}
	if len(req.RegisterCode) > 64 || len(req.Hostname) > 255 || len(req.HostFingerprint) != 64 {
		return nil, ErrInvalidRegisterReq
	}
	// 字段净化(审查 MEDIUM #8): 防日志注入与存储膨胀
	if !safeLabel(req.Hostname, 255) || !safeLabel(req.OS, 64) ||
		!safeLabel(req.Arch, 32) || !safeLabel(req.AgentVersion, 32) {
		return nil, ErrInvalidRegisterReq
	}
	if req.IPv4 != "" && net.ParseIP(req.IPv4) == nil {
		return nil, ErrInvalidRegisterReq
	}
	if req.IPv6 != "" && net.ParseIP(req.IPv6) == nil {
		return nil, ErrInvalidRegisterReq
	}

	codeHash := pkg.SHA256Hex(req.RegisterCode)
	found, expired, bindID, err := s.codes.GetActive(ctx, codeHash, s.now().Unix())
	if err != nil {
		if errors.Is(err, repository.ErrCodeUsed) {
			return nil, ErrCodeUsed
		}
		return nil, err
	}
	if !found {
		return nil, ErrCodeInvalid
	}
	if expired {
		return nil, ErrCodeExpired
	}

	// 指纹冲突预检(绑定模式排除自身; 全局 UNIQUE 兜底)
	if other, gerr := s.agents.GetByFingerprint(ctx, req.HostFingerprint); gerr == nil && other != nil && other.ID != bindID {
		return nil, ErrFingerprintConflict
	}

	// 主机指纹唯一约束(设计文档 7.5): 冲突返回 409 由管理员裁决
	if existing, gerr := s.agents.GetByFingerprint(ctx, req.HostFingerprint); gerr == nil && existing != nil {
		return nil, ErrFingerprintConflict
	}

	token, err := pkg.RandomToken()
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	resp := &model.RegisterResponse{Token: token, AgentID: 0}

	if bindID > 0 {
		// 绑定模式: 绑定到预创建节点(更新该行而非新建)
		if err := s.agents.UpdateBind(ctx, bindID, &model.Agent{
			TokenHash:       pkg.SHA256Hex(token),
			Hostname:        req.Hostname,
			OS:              req.OS,
			Arch:            req.Arch,
			AgentVersion:    req.AgentVersion,
			HostFingerprint: req.HostFingerprint,
			IPv4:            firstNonEmpty(req.IPv4, remoteIP),
			LastSeen:        now,
		}); err != nil {
			return nil, err
		}
		if consumed, cerr := s.codes.Consume(ctx, codeHash, bindID, now); cerr != nil || !consumed {
			return nil, ErrCodeUsed
		}
		resp.AgentID = bindID
		return resp, nil
	}

	agent := &model.Agent{
		TokenHash:       pkg.SHA256Hex(token),
		Hostname:        req.Hostname,
		OS:              req.OS,
		Arch:            req.Arch,
		AgentVersion:    req.AgentVersion,
		HostFingerprint: req.HostFingerprint,
		IPv4:            firstNonEmpty(req.IPv4, remoteIP),
		CreatedAt:       now,
		LastSeen:        now,
	}
	id, err := s.agents.Create(ctx, agent)
	if err != nil {
		return nil, err
	}
	consumed, cerr := s.codes.Consume(ctx, codeHash, id, now)
	if cerr != nil {
		_ = s.agents.DeleteCascade(ctx, id)
		return nil, cerr
	}
	if !consumed {
		_ = s.agents.DeleteCascade(ctx, id)
		return nil, ErrCodeUsed
	}
	resp.AgentID = id
	return resp, nil
}

// ListCodes 管理页注册码列表。
func (s *Registry) ListCodes(ctx context.Context) ([]model.RegisterCodeInfo, error) {
	return s.codes.List(ctx)
}

// DeleteCode 删除未使用的注册码。
func (s *Registry) DeleteCode(ctx context.Context, hash string) error {
	return s.codes.Delete(ctx, hash)
}

// SafeLabel 导出版(供 API 层复用)。
func SafeLabel(s string, max int) bool { return safeLabel(s, max) }

// safeLabel 可打印安全标签: 排除控制字符与空白(防日志注入), 限长。
func safeLabel(s string, max int) bool {
	if len(s) > max {
		return false
	}
	for _, c := range s {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func randomCode(n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(codeAlphabet)))
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("rand code: %w", err)
		}
		out[i] = codeAlphabet[v.Int64()]
	}
	return string(out), nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
