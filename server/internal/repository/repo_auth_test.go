package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminRepo_SingleAdminGuard(t *testing.T) {
	// 审查 MEDIUM #5 回归: NOT EXISTS 守卫保证并发/重复初始化只产生一个管理员
	db, err := Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := NewAdminRepo(db)

	if _, err := repo.Create(ctx, "admin", "hash1"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := repo.Create(ctx, "root", "hash2"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("second create err = %v, want ErrAdminExists", err)
	}
	if n, _ := repo.Count(ctx); n != 1 {
		t.Fatalf("admin count = %d, want 1", n)
	}
}

func TestSessionRepo_RevokeByHash(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sess.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := NewSessionRepo(db)
	now := time.Now().Unix()
	if err := repo.Create(ctx, "hash-a", now+3600, "1.2.3.4", "ua"); err != nil {
		t.Fatal(err)
	}
	if active, _ := repo.IsActive(ctx, "hash-a", now); !active {
		t.Fatal("session should be active")
	}
	if err := repo.RevokeByHash(ctx, "hash-a"); err != nil {
		t.Fatal(err)
	}
	if active, _ := repo.IsActive(ctx, "hash-a", now); active {
		t.Fatal("session should be revoked")
	}
	if _, err := repo.FindByHash(ctx, "hash-a"); err != nil {
		t.Fatalf("FindByHash after revoke: %v", err)
	}
}
