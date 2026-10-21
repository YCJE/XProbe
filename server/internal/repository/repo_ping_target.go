package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"probe/server/internal/model"
)

type PingTargetRepo struct {
	db *sql.DB
}

func NewPingTargetRepo(db *sql.DB) *PingTargetRepo {
	return &PingTargetRepo{db: db}
}

var ErrPingTargetNotFound = errors.New("ping target not found")

const pingTargetCols = `id, target, name, COALESCE(region,''), COALESCE(isp,''),
	ip_version, protocol, is_default, enabled, created_at`

func (r *PingTargetRepo) ListAll(ctx context.Context) ([]model.PingTarget, error) {
	return r.list(ctx, `SELECT `+pingTargetCols+` FROM ping_targets ORDER BY id`)
}

func (r *PingTargetRepo) Get(ctx context.Context, id int64) (*model.PingTarget, error) {
	targets, err := r.list(ctx, `SELECT `+pingTargetCols+` FROM ping_targets WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, ErrPingTargetNotFound
	}
	return &targets[0], nil
}

func (r *PingTargetRepo) Create(ctx context.Context, t *model.PingTarget, at time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO ping_targets
		(target, name, region, isp, ip_version, protocol, is_default, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		t.Target, t.Name, t.Region, t.ISP, t.IPVersion, t.Protocol, boolInt(t.Enabled), at.Unix())
	if err != nil {
		return 0, fmt.Errorf("insert ping target: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	t.ID = id
	t.CreatedAt = at
	return id, nil
}

func (r *PingTargetRepo) Update(ctx context.Context, t *model.PingTarget) error {
	res, err := r.db.ExecContext(ctx, `UPDATE ping_targets SET
		target = ?, name = ?, region = ?, isp = ?, ip_version = ?, protocol = ?, enabled = ?
		WHERE id = ?`,
		t.Target, t.Name, t.Region, t.ISP, t.IPVersion, t.Protocol, boolInt(t.Enabled), t.ID)
	if err != nil {
		return fmt.Errorf("update ping target: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPingTargetNotFound
	}
	return nil
}

func (r *PingTargetRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM ping_targets WHERE id = ? AND is_default = 0`, id)
	if err != nil {
		return fmt.Errorf("delete ping target: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPingTargetNotFound
	}
	return nil
}

func (r *PingTargetRepo) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ping_targets`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ListEnabled returns the read-only probe configuration sent to agents.
func (r *PingTargetRepo) ListEnabled(ctx context.Context) ([]model.PingTarget, error) {
	return r.list(ctx, `SELECT `+pingTargetCols+` FROM ping_targets WHERE enabled = 1 ORDER BY id`)
}

func (r *PingTargetRepo) list(ctx context.Context, query string, args ...any) ([]model.PingTarget, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list ping targets: %w", err)
	}
	defer rows.Close()
	var out []model.PingTarget
	for rows.Next() {
		var t model.PingTarget
		var isDefault, enabled int
		var createdAt int64
		if err := rows.Scan(&t.ID, &t.Target, &t.Name, &t.Region, &t.ISP,
			&t.IPVersion, &t.Protocol, &isDefault, &enabled, &createdAt); err != nil {
			return nil, fmt.Errorf("scan ping target: %w", err)
		}
		t.IsDefault = isDefault != 0
		t.Enabled = enabled != 0
		t.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}
