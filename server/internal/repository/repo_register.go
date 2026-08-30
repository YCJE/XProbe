package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RegisterCodeRepo 管理注册码(库中仅存 SHA256 哈希, S9)。
type RegisterCodeRepo struct {
	db *sql.DB
}

func NewRegisterCodeRepo(db *sql.DB) *RegisterCodeRepo { return &RegisterCodeRepo{db: db} }

// Create 写入注册码哈希与过期时间。
func (r *RegisterCodeRepo) Create(ctx context.Context, codeHash string, expiresAt int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO register_codes (code_hash, created_at, expires_at, used) VALUES (?, ?, ?, 0)`,
		codeHash, time.Now().Unix(), expiresAt)
	if err != nil {
		return fmt.Errorf("insert register_code: %w", err)
	}
	return nil
}

// CountActive 统计未使用且未过期的注册码数量(上限 5 个, 设计文档 4.2)。
func (r *RegisterCodeRepo) CountActive(ctx context.Context, now int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM register_codes WHERE used = 0 AND expires_at > ?`, now).Scan(&n)
	return n, err
}

// ErrCodeUnknown 注册码不存在(含已过期视角区分由调用方用 Get 完成)。
var ErrCodeUnknown = errors.New("repository: register code unknown")

// ErrCodeUsed 注册码已被消费。
var ErrCodeUsed = errors.New("repository: register code already used")

// GetActive 返回未使用且未过期的注册码哈希记录(过期视为无效但与已使用区分错误码)。
// 返回 (found, expired)。
func (r *RegisterCodeRepo) GetActive(ctx context.Context, codeHash string, now int64) (found, expired bool, err error) {
	var expires int64
	var used int
	qerr := r.db.QueryRowContext(ctx,
		`SELECT expires_at, used FROM register_codes WHERE code_hash = ?`, codeHash).
		Scan(&expires, &used)
	if errors.Is(qerr, sql.ErrNoRows) {
		return false, false, nil
	}
	if qerr != nil {
		return false, false, qerr
	}
	if used == 1 {
		return true, false, ErrCodeUsed
	}
	if now >= expires {
		return true, true, nil
	}
	return true, false, nil
}

// Consume 一次性消费注册码(乐观更新: 仅未使用时生效), 返回是否消费成功。
func (r *RegisterCodeRepo) Consume(ctx context.Context, codeHash string, agentID, now int64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE register_codes SET used = 1, used_by_agent_id = ? WHERE code_hash = ? AND used = 0`,
		agentID, codeHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// DeleteExpired 清理已过期注册码(含已使用), 由定时任务调用。
func (r *RegisterCodeRepo) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM register_codes WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
