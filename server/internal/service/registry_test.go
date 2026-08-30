package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

func newTestRegistry(t *testing.T) (*Registry, *repository.AgentRepo) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewRegistry(repository.NewAgentRepo(db), repository.NewRegisterCodeRepo(db)), repository.NewAgentRepo(db)
}

func regReq(code, fp string) *model.RegisterRequest {
	return &model.RegisterRequest{
		RegisterCode:    code,
		Hostname:        "web-01",
		HostFingerprint: fp,
		OS:              "Ubuntu 22.04",
		Arch:            "x86_64",
		AgentVersion:    "0.1.0-dev",
	}
}

func TestRegistry_RegisterHappyPath(t *testing.T) {
	reg, agents := newTestRegistry(t)
	ctx := context.Background()

	code, expires, err := reg.IssueCode(ctx)
	if err != nil {
		t.Fatalf("IssueCode: %v", err)
	}
	if time.Until(expires) <= 0 || time.Until(expires) > 16*time.Minute {
		t.Fatalf("expires = %v, want ~15min", expires)
	}

	resp, err := reg.Register(ctx, regReq(code, fpHash("fp-a")), "203.0.113.9")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(resp.Token) != 64 {
		t.Fatalf("token = %q, want 64 hex", resp.Token)
	}
	// S9: 数据库只存哈希, 不存原文
	agent, err := agents.Get(ctx, resp.AgentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.TokenHash != sha256Of(resp.Token) {
		t.Fatalf("token_hash = %s, want sha256(token)", agent.TokenHash)
	}
	if agent.IPv4 != "203.0.113.9" {
		t.Fatalf("ipv4 = %s, want remote ip", agent.IPv4)
	}
}

func TestRegistry_CodeOneTimeAndInvalid(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	code, _, _ := reg.IssueCode(ctx)
	if _, err := reg.Register(ctx, regReq(code, fpHash("fp-1")), ""); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	// 二次使用: 已消费 → ErrCodeUsed(401)
	if _, err := reg.Register(ctx, regReq(code, fpHash("fp-2")), ""); err != ErrCodeUsed {
		t.Fatalf("second Register err = %v, want ErrCodeUsed", err)
	}
	// 未知码 → ErrCodeInvalid
	if _, err := reg.Register(ctx, regReq("NOPE0NOPE0", fpHash("fp-3")), ""); err != ErrCodeInvalid {
		t.Fatalf("unknown code err = %v, want ErrCodeInvalid", err)
	}
}

func TestRegistry_CodeExpiry(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()
	reg.SetCodeTTL(-time.Minute) // 立即过期

	code, _, _ := reg.IssueCode(ctx)
	if _, err := reg.Register(ctx, regReq(code, fpHash("fp-e")), ""); err != ErrCodeExpired {
		t.Fatalf("err = %v, want ErrCodeExpired", err)
	}
}

func TestRegistry_CodeLimit5(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, _, err := reg.IssueCode(ctx); err != nil {
			t.Fatalf("issue %d: %v", i+1, err)
		}
	}
	if _, _, err := reg.IssueCode(ctx); err != ErrTooManyCodes {
		t.Fatalf("err = %v, want ErrTooManyCodes", err)
	}
}

func TestRegistry_FingerprintConflict409(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	code1, _, _ := reg.IssueCode(ctx)
	if _, err := reg.Register(ctx, regReq(code1, fpHash("same-fp")), ""); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 第二台机器同指纹(同模板批量 VPS): 409
	code2, _, _ := reg.IssueCode(ctx)
	if _, err := reg.Register(ctx, regReq(code2, fpHash("same-fp")), ""); err != ErrFingerprintConflict {
		t.Fatalf("err = %v, want ErrFingerprintConflict", err)
	}
}

func TestRegistry_InvalidRequests(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()
	cases := []*model.RegisterRequest{
		nil,
		{Hostname: "h", HostFingerprint: fpHash("f")},                // 无码
		{RegisterCode: "X", Hostname: "h", HostFingerprint: "short"}, // 指纹长度错
		{RegisterCode: "X", HostFingerprint: fpHash("f")},            // 无主机名
	}
	for i, c := range cases {
		if _, err := reg.Register(ctx, c, ""); err != ErrInvalidRegisterReq {
			t.Fatalf("case %d err = %v, want ErrInvalidRegisterReq", i, err)
		}
	}
}
