package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func sampleAgent(tokenHash, fp string) *model.Agent {
	now := time.Now().Unix()
	return &model.Agent{
		TokenHash:       tokenHash,
		Hostname:        "web-server-01",
		DisplayName:     "US-LAX-01",
		OS:              "Ubuntu 22.04",
		Arch:            "x86_64",
		AgentVersion:    "0.1.0-dev",
		HostFingerprint: fp,
		IPv4:            "1.2.3.4",
		CreatedAt:       now,
		LastSeen:        now,
	}
}

func TestOpen_WALMode(t *testing.T) {
	db := newTestDB(t)
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %s, want wal", mode)
	}
}

func TestAgentRepo_CRUDRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := NewAgentRepo(db)

	a := sampleAgent("hash-token-1", "fp-1")
	id, err := repo.Create(ctx, a)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TokenHash != "hash-token-1" || got.Hostname != "web-server-01" ||
		got.DisplayName != "US-LAX-01" || got.OS != "Ubuntu 22.04" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	if err := repo.Touch(ctx, id, true, time.Now().Unix()+10); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, _ = repo.Get(ctx, id)
	if !got.Online {
		t.Fatal("agent should be online after Touch(true)")
	}

	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
}

func TestAgentRepo_GetByTokenHashAndFingerprint(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := NewAgentRepo(db)

	if _, err := repo.Create(ctx, sampleAgent("hash-a", "fp-a")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got, err := repo.GetByTokenHash(ctx, "hash-a"); err != nil || got.HostFingerprint != "fp-a" {
		t.Fatalf("GetByTokenHash = %+v, %v", got, err)
	}
	if got, err := repo.GetByFingerprint(ctx, "fp-a"); err != nil || got.ID == 0 {
		t.Fatalf("GetByFingerprint = %+v, %v", got, err)
	}
	if _, err := repo.GetByTokenHash(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing token should return ErrNotFound, got %v", err)
	}
}

func TestAgentRepo_TokenHashUnique(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := NewAgentRepo(db)

	if _, err := repo.Create(ctx, sampleAgent("dup", "fp-1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Create(ctx, sampleAgent("dup", "fp-2")); err == nil {
		t.Fatal("duplicate token_hash should violate UNIQUE")
	}
}

func TestAgentRepo_DeleteCascade(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := NewAgentRepo(db)

	id, err := repo.Create(ctx, sampleAgent("hash-x", "fp-x"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().Unix()
	// 先建一条告警规则(alert_history.rule_id 有外键约束)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO alert_rules (name, metric, operator, threshold, duration, created_at) VALUES ('cpu', 'cpu_usage', '>', 80, 300, ?)`,
		now); err != nil {
		t.Fatalf("seed alert_rules: %v", err)
	}
	// 灌入级联数据
	for _, q := range []string{
		`INSERT INTO metric_records (agent_id, timestamp) VALUES (?, ?)`,
		`INSERT INTO metric_records_daily (agent_id, date) VALUES (?, '2026-08-31')`,
		`INSERT INTO traffic_monthly (agent_id, month, rx_bytes, tx_bytes) VALUES (?, '2026-08', 1, 1)`,
		`INSERT INTO alert_history (rule_id, agent_id, status, started_at, updated_at) VALUES (1, ?, 'FIRING', ?, ?)`,
	} {
		if _, err := db.ExecContext(ctx, q, id, now, now); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := repo.DeleteCascade(ctx, id); err != nil {
		t.Fatalf("DeleteCascade: %v", err)
	}
	if _, err := repo.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent should be gone, got %v", err)
	}
	for _, table := range []string{"metric_records", "metric_records_daily", "traffic_monthly", "alert_history"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("table %s not cascaded, %d rows left", table, n)
		}
	}
}
