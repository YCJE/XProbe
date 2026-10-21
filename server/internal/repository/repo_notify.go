package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"probe/server/internal/model"
)

var ErrChannelNotFound = errors.New("notify channel not found")

type NotifyRepo struct {
	db *sql.DB
}

func NewNotifyRepo(db *sql.DB) *NotifyRepo {
	return &NotifyRepo{db: db}
}

func (r *NotifyRepo) List(ctx context.Context) ([]model.NotifyChannel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, type, config, created_at
		FROM notify_channels ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list notify channels: %w", err)
	}
	defer rows.Close()
	var out []model.NotifyChannel
	for rows.Next() {
		var c model.NotifyChannel
		var created int64
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &created); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(created, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *NotifyRepo) Get(ctx context.Context, id int64) (*model.NotifyChannel, error) {
	var c model.NotifyChannel
	var created int64
	err := r.db.QueryRowContext(ctx, `SELECT id, name, type, config, created_at
		FROM notify_channels WHERE id = ?`, id).Scan(&c.ID, &c.Name, &c.Type, &c.Config, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt = time.Unix(created, 0)
	return &c, nil
}

func (r *NotifyRepo) Create(ctx context.Context, c *model.NotifyChannel, at time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO notify_channels
		(name, type, config, created_at) VALUES (?, ?, ?, ?)`,
		c.Name, c.Type, c.Config, at.Unix())
	if err != nil {
		return 0, fmt.Errorf("insert notify channel: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID = id
	return id, nil
}

func (r *NotifyRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM notify_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete notify channel: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChannelNotFound
	}
	return nil
}
