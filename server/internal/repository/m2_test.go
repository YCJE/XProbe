package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"probe/server/internal/model"
	"probe/server/internal/security"
)

func TestAdminAndSessionRepositories(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	admins := NewAdminRepo(db)
	admin := &model.Admin{Username: "admin", PasswordHash: "hash", CreatedAt: time.Now()}
	if _, err := admins.Create(ctx, admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if n, err := admins.Count(ctx); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
	got, err := admins.GetByUsername(ctx, "admin")
	if err != nil || got.ID != admin.ID {
		t.Fatalf("get admin: %v %v", got, err)
	}
	if _, err := admins.GetByUsername(ctx, "missing"); !errors.Is(err, ErrAdminNotFound) {
		t.Fatalf("expected ErrAdminNotFound, got %v", err)
	}

	sessions := NewSessionRepo(db)
	now := time.Now()
	s := &model.Session{
		ID:            "sid",
		TokenHash:     security.HashToken("session-token"),
		AdminID:       admin.ID,
		ExpiresAt:     now.Add(7 * 24 * time.Hour),
		IdleExpiresAt: now.Add(2 * time.Hour),
		CreatedAt:     now,
		IP:            "203.0.113.7",
		UserAgent:     "test",
	}
	if err := sessions.Create(ctx, s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sessions.Create(ctx, s); err == nil {
		t.Fatal("duplicate session ID was accepted")
	}
	loaded, err := sessions.GetByTokenHash(ctx, s.TokenHash)
	if err != nil || loaded.ID != s.ID || loaded.AdminID != admin.ID || loaded.Revoked {
		t.Fatalf("get session: %+v %v", loaded, err)
	}
	newIdle := now.Add(3 * time.Hour)
	if err := sessions.Touch(ctx, s.ID, newIdle, now.Add(time.Minute)); err != nil {
		t.Fatalf("touch: %v", err)
	}
	loaded, _ = sessions.GetByTokenHash(ctx, s.TokenHash)
	if loaded.IdleExpiresAt.Unix() != newIdle.Unix() || loaded.LastSeenAt == nil {
		t.Fatalf("touch not persisted: %+v", loaded)
	}
	if err := sessions.Revoke(ctx, s.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	loaded, _ = sessions.GetByTokenHash(ctx, s.TokenHash)
	if !loaded.Revoked {
		t.Fatal("session was not revoked")
	}
	if err := sessions.DeleteExpired(ctx, now.Add(8*24*time.Hour)); err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if _, err := sessions.GetByTokenHash(ctx, s.TokenHash); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected expired session to be deleted, got %v", err)
	}
}

func TestRegisterCodeAndAgentRegistration(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	codes := NewRegisterCodeRepo(db)
	code := "ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ"
	codeHash := security.HashRegistrationCode(code)
	id, err := codes.Create(ctx, codeHash, now, now.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("create code: %v", err)
	}
	if n, err := codes.CountUnused(ctx, now); err != nil || n != 1 {
		t.Fatalf("count unused = %d, %v", n, err)
	}
	list, err := codes.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].CodeHash != codeHash {
		t.Fatalf("list codes: %+v %v", list, err)
	}

	agents := NewAgentRepo(db)
	agent := &model.Agent{
		TokenHash:       security.HashToken("agent-token"),
		Hostname:        "web-01",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "0.2.0",
		HostFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	agentID, err := agents.Register(ctx, codeHash, agent, now)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if agentID != agent.ID || agent.ID == 0 {
		t.Fatalf("agent ID = %d, want %d", agentID, agent.ID)
	}
	if _, err := agents.Register(ctx, codeHash, &model.Agent{
		TokenHash:       security.HashToken("other-token"),
		Hostname:        "web-02",
		HostFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}, now); !errors.Is(err, ErrRegisterCodeInvalid) {
		t.Fatalf("expected spent code to be rejected, got %v", err)
	}

	// A fresh code with a duplicate fingerprint is rejected without spending it.
	code2 := security.HashRegistrationCode("SECOND-CODE")
	code2ID, err := codes.Create(ctx, code2, now, now.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agents.Register(ctx, code2, &model.Agent{
		TokenHash:       security.HashToken("third-token"),
		Hostname:        "web-03",
		HostFingerprint: agent.HostFingerprint,
	}, now); !errors.Is(err, ErrFingerprintConflict) {
		t.Fatalf("expected fingerprint conflict, got %v", err)
	}
	var used int
	if err := db.QueryRowContext(ctx, `SELECT used FROM register_codes WHERE id = ?`, code2ID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 0 {
		t.Fatal("a failed registration spent the code")
	}
}

func TestPingTargetUpdateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := NewPingTargetRepo(db)
	target := &model.PingTarget{Target: "1.1.1.1", Name: "CF", IPVersion: 4, Protocol: "icmp", Enabled: true}
	if _, err := repo.Create(ctx, target, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, target); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if err := repo.Update(ctx, target); err != nil {
		t.Fatalf("idempotent update: %v", err)
	}
}

func TestSettingsAndPingTargets(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	settings := NewSettingsRepo(db)
	if _, err := settings.Get(ctx, "theme"); !errors.Is(err, ErrSettingNotFound) {
		t.Fatalf("expected missing setting, got %v", err)
	}
	if err := settings.Set(ctx, "theme", "glass", time.Now()); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if got, err := settings.Get(ctx, "theme"); err != nil || got != "glass" {
		t.Fatalf("get setting = %q, %v", got, err)
	}
	if err := settings.Set(ctx, "theme", "minimal", time.Now()); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	if got, _ := settings.Get(ctx, "theme"); got != "minimal" {
		t.Fatalf("setting not updated: %q", got)
	}

	targets, err := NewPingTargetRepo(db).ListEnabled(ctx)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 5 {
		t.Fatalf("seeded targets = %d, want 5", len(targets))
	}
}
