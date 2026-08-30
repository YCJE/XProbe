package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// 认证错误 → HTTP 映射。
var (
	ErrBadCreds       = errors.New("auth: bad credentials")    // 401
	ErrLocked         = errors.New("auth: account locked")     // 423
	ErrSetupDone      = errors.New("auth: setup already done") // 409
	ErrWeakPass       = errors.New("auth: weak password")      // 400
	ErrTooManyAuth    = errors.New("auth: too many attempts")  // 429
	ErrSessionRevoked = errors.New("auth: session revoked")    // 401
)

const maxLoginFailures = 10 // 连续 10 次失败锁定 15 分钟(设计文档 7.3)

type failState struct {
	count       int
	lockedUntil time.Time
}

// Auth 管理员认证: 初始化/登录(锁定)/会话吊销/静默续期。
type Auth struct {
	admins   *repository.AdminRepo
	sessions *repository.SessionRepo
	jwt      *pkg.JWTManager
	limiter  *pkg.Limiter // 登录 5 次/分钟/IP

	mu       sync.Mutex
	failures map[string]*failState
	now      func() time.Time
}

func NewAuth(admins *repository.AdminRepo, sessions *repository.SessionRepo,
	jwt *pkg.JWTManager, loginLimiter *pkg.Limiter) *Auth {
	return &Auth{
		admins: admins, sessions: sessions, jwt: jwt, limiter: loginLimiter,
		failures: map[string]*failState{}, now: time.Now,
	}
}

// Setup 首次初始化管理员(仅 admin 表为空时可用; 无默认密码, 设计文档 7.3)。
func (s *Auth) Setup(ctx context.Context, username, password, ua string) error {
	if n, err := s.admins.Count(ctx); err != nil {
		return err
	} else if n > 0 {
		return ErrSetupDone
	}
	if err := pkg.ValidatePasswordPolicy(password); err != nil {
		return ErrWeakPass
	}
	hash, err := pkg.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := s.admins.Create(ctx, username, hash); err != nil {
		return err
	}
	return nil
}

// SetupDone 报告是否已完成初始化。
func (s *Auth) SetupDone(ctx context.Context) bool {
	n, err := s.admins.Count(ctx)
	return err != nil || n > 0
}

// Login 登录: IP 限速 + 账号锁定 + bcrypt 校验 → JWT 会话。
func (s *Auth) Login(ctx context.Context, username, password, ip, ua string) (string, time.Time, error) {
	if s.limiter != nil && !s.limiter.Allow(ip) {
		return "", time.Time{}, ErrTooManyAuth
	}
	s.mu.Lock()
	st := s.failures[username]
	if st == nil {
		st = &failState{}
		s.failures[username] = st
	}
	if s.now().Before(st.lockedUntil) {
		s.mu.Unlock()
		return "", time.Time{}, ErrLocked
	}
	s.mu.Unlock()

	id, hash, err := s.admins.GetPasswordHash(ctx, username)
	if err != nil || !pkg.CheckPassword(hash, password) {
		s.mu.Lock()
		st.count++
		if st.count >= maxLoginFailures {
			st.lockedUntil = s.now().Add(15 * time.Minute)
			st.count = 0
		}
		s.mu.Unlock()
		if err != nil && !errors.Is(err, repository.ErrAdminNotFound) {
			return "", time.Time{}, err
		}
		return "", time.Time{}, ErrBadCreds
	}
	s.mu.Lock()
	delete(s.failures, username)
	s.mu.Unlock()

	return s.createSession(ctx, id, username, ua)
}

// createSession 签发 JWT 并登记会话(仅存哈希, S9)。
func (s *Auth) createSession(ctx context.Context, adminID int64, username, ua string) (string, time.Time, error) {
	token, exp, err := s.jwt.Issue(adminID, username)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := s.sessions.Create(ctx, pkg.SHA256Hex(token), exp.Unix(), "", ua); err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

// Validate 校验 Token 签名与会话状态, 返回 Claims。
func (s *Auth) Validate(ctx context.Context, token string) (*pkg.Claims, error) {
	claims, err := s.jwt.Parse(token)
	if err != nil {
		return nil, err
	}
	active, err := s.sessions.IsActive(ctx, pkg.SHA256Hex(token), s.now().Unix())
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrSessionRevoked
	}
	return claims, nil
}

// RenewSession 静默续期: 签发新会话并吊销旧会话(设计文档 7.3, 剩余 <30min 时由中间件触发)。
func (s *Auth) RenewSession(ctx context.Context, oldToken, username, ua string) (string, time.Time, error) {
	id, _, err := s.admins.GetPasswordHash(ctx, username)
	if err != nil {
		return "", time.Time{}, err
	}
	token, exp, err := s.createSession(ctx, id, username, ua)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := s.sessions.RevokeByHash(ctx, pkg.SHA256Hex(oldToken)); err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

// Logout 吊销当前会话。
func (s *Auth) Logout(ctx context.Context, token string) error {
	return s.sessions.RevokeByHash(ctx, pkg.SHA256Hex(token))
}

// ResetPassword 重置密码并吊销全部会话(reset-password CLI 语义, 设计文档 7.3.1)。
func (s *Auth) ResetPassword(ctx context.Context, username, newPassword string) error {
	if err := pkg.ValidatePasswordPolicy(newPassword); err != nil {
		return ErrWeakPass
	}
	hash, err := pkg.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.admins.UpdatePassword(ctx, username, hash); err != nil {
		return err
	}
	return s.sessions.RevokeAll(ctx)
}
