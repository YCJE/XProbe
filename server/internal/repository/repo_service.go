package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

// ServiceRepo 服务监控拨测配置与日汇总(Nezha 对标)。
type ServiceRepo struct{ db *sql.DB }

func NewServiceRepo(db *sql.DB) *ServiceRepo { return &ServiceRepo{db: db} }

func scanService(row interface{ Scan(...any) error }) (*model.Service, error) {
	var v model.Service
	var port, channel sql.NullInt64
	var path sql.NullString
	if err := row.Scan(&v.ID, &v.Name, &v.Type, &v.Target, &port, &path,
		&v.IntervalSec, &v.Enabled, &channel, &v.CreatedAt); err != nil {
		return nil, err
	}
	if port.Valid {
		v.Port = int(port.Int64)
	}
	v.Path = path.String
	if channel.Valid {
		v.NotifyChannelID = &channel.Int64
	}
	return &v, nil
}

const serviceCols = `id, name, type, target, port, path, interval_sec, enabled, notify_channel_id, created_at`

func (r *ServiceRepo) List(ctx context.Context) ([]model.Service, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+serviceCols+` FROM services ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Service{}
	for rows.Next() {
		v, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *ServiceRepo) ListEnabled(ctx context.Context) ([]model.Service, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+serviceCols+` FROM services WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Service{}
	for rows.Next() {
		v, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *ServiceRepo) Create(ctx context.Context, v *model.Service) (int64, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO services
		(name, type, target, port, path, interval_sec, enabled, notify_channel_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.Type, v.Target, v.Port, v.Path, v.IntervalSec, boolToInt(v.Enabled),
		v.NotifyChannelID, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *ServiceRepo) Update(ctx context.Context, v *model.Service) error {
	res, err := r.db.ExecContext(ctx, `UPDATE services SET
		name = ?, type = ?, target = ?, port = ?, path = ?, interval_sec = ?, enabled = ?, notify_channel_id = ?
		WHERE id = ?`,
		v.Name, v.Type, v.Target, v.Port, v.Path, v.IntervalSec, boolToInt(v.Enabled), v.NotifyChannelID, v.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ServiceRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM services WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertDaily 日汇总落库(在线率)。
func (r *ServiceRepo) UpsertDaily(ctx context.Context, serviceID int64, d model.ServiceDaily) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO service_daily (service_id, date, total, ok, up_ratio)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(service_id, date) DO UPDATE SET
			total = excluded.total,
			ok = excluded.ok,
			up_ratio = excluded.up_ratio`,
		serviceID, d.Date, d.Total, d.Ok, d.UpRatio)
	return err
}

// ListDaily 近 N 天日汇总(旧→新)。
func (r *ServiceRepo) ListDaily(ctx context.Context, serviceID int64, days int) ([]model.ServiceDaily, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT date, total, ok, up_ratio FROM service_daily
		WHERE service_id = ? ORDER BY date DESC LIMIT ?`, serviceID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ServiceDaily{}
	for rows.Next() {
		var d model.ServiceDaily
		if err := rows.Scan(&d.Date, &d.Total, &d.Ok, &d.UpRatio); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
