package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

// AdminRepo 管理员账户(单管理员, 设计文档 7.3)。
type AdminRepo struct{ db *sql.DB }

func NewAdminRepo(db *sql.DB) *AdminRepo { return &AdminRepo{db: db} }

func (r *AdminRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin`).Scan(&n)
	return n, err
}

var ErrAdminExists = errors.New("repository: admin already exists")

// Create 写入管理员; 用 NOT EXISTS 守卫保证单管理员(防并发初始化竞态产生多账号)。
func (r *AdminRepo) Create(ctx context.Context, username, passwordHash string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO admin (username, password_hash, created_at)
		 SELECT ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM admin)`,
		username, passwordHash, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, ErrAdminExists
	}
	return res.LastInsertId()
}

var ErrAdminNotFound = errors.New("repository: admin not found")

// GetPasswordHash 返回 (id, password_hash)。
func (r *AdminRepo) GetPasswordHash(ctx context.Context, username string) (int64, string, error) {
	var id int64
	var hash string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM admin WHERE username = ?`, username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrAdminNotFound
	}
	return id, hash, err
}

// UpdatePassword 更新密码哈希并吊销全部会话(reset-password CLI 语义)。
func (r *AdminRepo) UpdatePassword(ctx context.Context, username, passwordHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE admin SET password_hash = ? WHERE username = ?`, passwordHash, username)
	return err
}

// SessionRepo 登录会话(JWT 吊销依据; token 仅存哈希, S9)。
type SessionRepo struct{ db *sql.DB }

func NewSessionRepo(db *sql.DB) *SessionRepo { return &SessionRepo{db: db} }

func (r *SessionRepo) Create(ctx context.Context, tokenHash string, expiresAt int64, ip, ua string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, created_at, expires_at, revoked, ip, user_agent)
		 VALUES (?, ?, ?, 0, ?, ?)`, tokenHash, time.Now().Unix(), expiresAt, ip, ua)
	return err
}

func (r *SessionRepo) IsActive(ctx context.Context, tokenHash string, now int64) (bool, error) {
	var revoked int
	err := r.db.QueryRowContext(ctx,
		`SELECT revoked FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, now).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return revoked == 0, nil
}

func (r *SessionRepo) ListActive(ctx context.Context, now int64) ([]model.SessionInfo, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, created_at, expires_at, COALESCE(ip,''), COALESCE(user_agent,'')
		 FROM sessions WHERE expires_at > ? AND revoked = 0 ORDER BY created_at DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SessionInfo{}
	for rows.Next() {
		var s model.SessionInfo
		var created, expires int64
		if err := rows.Scan(&s.ID, &created, &expires, &s.IP, &s.UserAgent); err != nil {
			return nil, err
		}
		s.CreatedAt = time.Unix(created, 0)
		s.ExpiresAt = time.Unix(expires, 0)
		out = append(out, s)
	}
	return out, rows.Err()
}

// FindByHash 按 Token 哈希查会话(标记"当前会话")。
func (r *SessionRepo) FindByHash(ctx context.Context, tokenHash string) (*model.SessionInfo, error) {
	var s model.SessionInfo
	var created, expires int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, created_at, expires_at, COALESCE(ip,''), COALESCE(user_agent,'')
		 FROM sessions WHERE token_hash = ?`, tokenHash).
		Scan(&s.ID, &created, &expires, &s.IP, &s.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.CreatedAt = time.Unix(created, 0)
	s.ExpiresAt = time.Unix(expires, 0)
	return &s, nil
}

// Revoke 吊销指定会话。
func (r *SessionRepo) Revoke(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET revoked = 1 WHERE id = ?`, id)
	return err
}

// RevokeByHash 按 Token 哈希吊销会话(续期/登出)。
func (r *SessionRepo) RevokeByHash(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET revoked = 1 WHERE token_hash = ?`, tokenHash)
	return err
}

// RevokeAll 吊销全部会话(登出所有设备/密码重置后)。
func (r *SessionRepo) RevokeAll(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET revoked = 1 WHERE revoked = 0`)
	return err
}

// DeleteExpired 清理已过期记录。
func (r *SessionRepo) DeleteExpired(ctx context.Context, now int64) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
