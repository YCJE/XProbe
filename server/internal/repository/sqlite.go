// Package repository 实现 Server 数据层: SQLite 连接管理、内存环形缓冲、各实体 CRUD。
package repository

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动, 无 cgo, 保障单命令交叉编译(设计文档 3.2)
)

//go:embed schema.sql
var schemaSQL string

// Open 打开 SQLite 并应用 PRAGMA(设计文档 8.2: WAL + busy_timeout)。
// MaxOpenConns=1: 单连接规避 SQLITE_BUSY——写入压力已被环形缓冲+聚合吸收,
// 本库只承载低频聚合/告警/元数据写与少量读。
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// Migrate 创建全部表与索引(IF NOT EXISTS, 幂等); 随后对旧库追加增量列(ALTER 失败视为已存在)。
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	for _, alt := range incrementalALTERs {
		_, _ = db.ExecContext(ctx, alt) // duplicate column → 忽略
	}
	return nil
}

// incrementalALTERs 旧库升级(v1.4 地图坐标列)。
var incrementalALTERs = []string{
	"ALTER TABLE agents ADD COLUMN geo_lat REAL",
	"ALTER TABLE agents ADD COLUMN geo_lon REAL",
	"ALTER TABLE agents ADD COLUMN notes TEXT",
	"ALTER TABLE register_codes ADD COLUMN bind_agent_id INTEGER",
}
