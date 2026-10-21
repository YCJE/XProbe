package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"probe/common/wire"
	"probe/server/internal/model"
	"probe/server/internal/security"
)

func TestMigrateAndAgentCRUD(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Idempotent: second run must be a no-op.
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}

	repo := NewAgentRepo(db)

	tokenHash := security.HashToken("tok-1")
	id, err := repo.Create(ctx, &model.Agent{
		TokenHash: tokenHash, Hostname: "web-01", OS: "linux", Arch: "amd64",
		AgentVersion: "0.1.0", HostFingerprint: "fp-1", TagIDs: "[]",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Hostname != "web-01" || got.TokenHash != tokenHash || got.Online {
		t.Fatalf("unexpected agent: %+v", got)
	}

	var stored string
	if err := db.QueryRowContext(ctx, `SELECT token_hash FROM agents WHERE id = ?`, id).Scan(&stored); err != nil {
		t.Fatalf("read stored token hash: %v", err)
	}
	if stored != tokenHash || stored == "tok-1" {
		t.Fatalf("database stored %q, want a hash and never the plaintext token", stored)
	}

	byToken, err := repo.GetByTokenHash(ctx, tokenHash)
	if err != nil || byToken.ID != id {
		t.Fatalf("get by token hash: %v %v", byToken, err)
	}

	if _, err := repo.GetByTokenHash(ctx, security.HashToken("missing")); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("expected ErrAgentNotFound, got %v", err)
	}

	if err := repo.UpdateMeta(ctx, &model.Agent{
		ID: id, DisplayName: "US-LAX-01", Region: "洛杉矶", CountryCode: "US",
		ISP: "Bandwagon", TagIDs: "[1,2]", GeoLat: 34.05, GeoLon: -118.24,
		TrafficQuota: 1 << 40,
	}); err != nil {
		t.Fatalf("update meta: %v", err)
	}

	got, _ = repo.GetByID(ctx, id)
	if got.DisplayName != "US-LAX-01" || got.CountryCode != "US" || got.GeoLat != 34.05 {
		t.Fatalf("meta not persisted: %+v", got)
	}

	if err := repo.SetOnline(ctx, id, true, time.Now()); err != nil {
		t.Fatalf("set online: %v", err)
	}
	if err := repo.Touch(ctx, id, "0.2.0", time.Now()); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, _ = repo.GetByID(ctx, id)
	if !got.Online || got.AgentVersion != "0.2.0" || got.LastSeen == nil {
		t.Fatalf("online/touch not persisted: %+v", got)
	}

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, id); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestDefaultPingTargetsSeeded(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ping_targets WHERE is_default = 1`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 seeded default targets, got %d", n)
	}
}

func TestCleanupKeepsOpenAlerts(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	agents := NewAgentRepo(db)
	agentID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "cleanup-a", Hostname: "cleanup-01",
		HostFingerprint: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "cleanup-b", Hostname: "cleanup-02",
		HostFingerprint: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	})
	if err != nil {
		t.Fatal(err)
	}
	alerts := NewAlertRepo(db)
	rule := &model.AlertRule{Name: "CPU", Metric: "cpu", Operator: ">", Threshold: 50,
		Enabled: true, Scope: model.AlertScope{Type: "all"}, CreatedAt: time.Now()}
	if _, err := alerts.CreateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -100)
	if err := alerts.UpsertHistory(ctx, rule.ID, agentID, model.AlertFiring, 80, old, true); err != nil {
		t.Fatal(err)
	}
	if err := alerts.UpsertHistory(ctx, rule.ID, otherID, model.AlertResolved, 0, old, true); err != nil {
		t.Fatal(err)
	}

	history := NewHistoryRepo(db)
	if err := history.UpsertMetric(ctx, agentID, old.Unix()/300*300, wire.Report{
		Data: wire.ReportData{CPU: wire.CPUMetrics{Usage: 10}, Memory: wire.MemoryMetrics{Total: 1, Used: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := history.InsertPings(ctx, agentID, []wire.PingStat{{
		Target: "1.1.1.1", Name: "CF", Method: "icmp", IPVersion: 4,
		AvgLatency: 1, MinLatency: 1, MaxLatency: 1, PacketsSent: 1, PacketsRecv: 1,
	}}, old); err != nil {
		t.Fatal(err)
	}
	if err := NewAuditRepo(db).Write(ctx, old, "admin", "test", "", ""); err != nil {
		t.Fatal(err)
	}

	if err := history.Cleanup(ctx, 90); err != nil {
		t.Fatal(err)
	}
	var firing, resolved, metrics, pings, audit int
	for query, dest := range map[string]*int{
		`SELECT COUNT(*) FROM alert_history WHERE status = 'FIRING'`:   &firing,
		`SELECT COUNT(*) FROM alert_history WHERE status = 'RESOLVED'`: &resolved,
		`SELECT COUNT(*) FROM metric_records`:                          &metrics,
		`SELECT COUNT(*) FROM ping_records`:                            &pings,
		`SELECT COUNT(*) FROM audit_log`:                               &audit,
	} {
		if err := db.QueryRowContext(ctx, query).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	if firing != 1 {
		t.Fatalf("open alert was deleted, firing rows = %d", firing)
	}
	if resolved != 0 || metrics != 0 || pings != 0 || audit != 0 {
		t.Fatalf("old resolved data survived: resolved=%d metrics=%d pings=%d audit=%d",
			resolved, metrics, pings, audit)
	}
}

func TestConsumeRecoveryCodesIsSingleUse(t *testing.T) {
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
	id, err := admins.Create(ctx, &model.Admin{
		Username: "admin", PasswordHash: "hash", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	original := `["hash-a","hash-b"]`
	updated := `["hash-b"]`
	if err := admins.SetRecoveryCodes(ctx, id, original); err != nil {
		t.Fatal(err)
	}
	ok, err := admins.ConsumeRecoveryCodes(ctx, id, original, updated)
	if err != nil || !ok {
		t.Fatalf("first consume = %v %v", ok, err)
	}
	ok, err = admins.ConsumeRecoveryCodes(ctx, id, original, updated)
	if err != nil || ok {
		t.Fatalf("second consume with a stale snapshot = %v %v, want false", ok, err)
	}
}

func TestOpenWithBusyTimeout(t *testing.T) {
	ctx := context.Background()
	db, err := OpenWithBusyTimeout(ctx, filepath.Join(t.TempDir(), "probe.db"), 12345)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var timeout int
	if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != 12345 {
		t.Fatalf("busy_timeout = %d, want 12345", timeout)
	}
}
