package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"probe/server/internal/model"
)

type RegisterCodeRepo struct {
	db *sql.DB
}

func NewRegisterCodeRepo(db *sql.DB) *RegisterCodeRepo {
	return &RegisterCodeRepo{db: db}
}

func (r *RegisterCodeRepo) Create(ctx context.Context, codeHash string, now, expiresAt time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO register_codes
		(code_hash, created_at, expires_at, used) VALUES (?, ?, ?, 0)`,
		codeHash, now.Unix(), expiresAt.Unix())
	if err != nil {
		return 0, fmt.Errorf("insert register code: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("register code id: %w", err)
	}
	return id, nil
}

func (r *RegisterCodeRepo) CountUnused(ctx context.Context, now time.Time) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM register_codes WHERE used = 0 AND expires_at > ?`,
		now.Unix()).Scan(&n); err != nil {
		return 0, fmt.Errorf("count unused register codes: %w", err)
	}
	return n, nil
}

func (r *RegisterCodeRepo) List(ctx context.Context) ([]model.RegisterCode, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, code_hash, created_at, expires_at,
		used, used_by_agent_id FROM register_codes ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list register codes: %w", err)
	}
	defer rows.Close()

	var out []model.RegisterCode
	for rows.Next() {
		var c model.RegisterCode
		var created, expires int64
		var used int
		var usedBy sql.NullInt64
		if err := rows.Scan(&c.ID, &c.CodeHash, &created, &expires, &used, &usedBy); err != nil {
			return nil, fmt.Errorf("scan register code: %w", err)
		}
		c.CreatedAt = time.Unix(created, 0)
		c.ExpiresAt = time.Unix(expires, 0)
		c.Used = used != 0
		if usedBy.Valid {
			id := usedBy.Int64
			c.UsedByAgentID = &id
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *RegisterCodeRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM register_codes WHERE id = ? AND used = 0`, id)
	if err != nil {
		return fmt.Errorf("delete register code: %w", err)
	}
	return nil
}
