package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens the SQLite database with WAL mode, busy timeout, foreign keys
// and NORMAL synchronous mode enabled (design doc §8.2). modernc.org/sqlite is
// used so the server cross-compiles without cgo.
func Open(ctx context.Context, dbPath string) (*sql.DB, error) {
	return OpenWithBusyTimeout(ctx, dbPath, 5000)
}

// OpenWithBusyTimeout honors storage.sqlite_busy_timeout_ms while keeping the
// test/helper default at five seconds.
func OpenWithBusyTimeout(ctx context.Context, dbPath string, busyTimeoutMS int) (*sql.DB, error) {
	if busyTimeoutMS <= 0 {
		busyTimeoutMS = 5000
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := "file:" + dbPath +
		fmt.Sprintf("?_pragma=busy_timeout(%d)", busyTimeoutMS) +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}
