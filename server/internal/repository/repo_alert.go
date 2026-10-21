package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"probe/server/internal/model"
)

var ErrAlertNotFound = errors.New("alert rule not found")

type AlertRepo struct {
	db *sql.DB
}

func NewAlertRepo(db *sql.DB) *AlertRepo {
	return &AlertRepo{db: db}
}

func (r *AlertRepo) ListRules(ctx context.Context) ([]model.AlertRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, metric, operator, threshold,
		duration, enabled, scope, notify_channel_ids, created_at
		FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()
	var out []model.AlertRule
	for rows.Next() {
		rule, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rule)
	}
	return out, rows.Err()
}

func (r *AlertRepo) GetRule(ctx context.Context, id int64) (*model.AlertRule, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, name, metric, operator, threshold,
		duration, enabled, scope, notify_channel_ids, created_at
		FROM alert_rules WHERE id = ?`, id)
	rule, err := scanAlertRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAlertNotFound
	}
	return rule, err
}

func (r *AlertRepo) CreateRule(ctx context.Context, rule *model.AlertRule) (int64, error) {
	scope, _ := json.Marshal(rule.Scope)
	channels, _ := json.Marshal(rule.NotifyChannelIDs)
	res, err := r.db.ExecContext(ctx, `INSERT INTO alert_rules
		(name, metric, operator, threshold, duration, enabled, scope, notify_channel_ids, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Name, rule.Metric, rule.Operator, rule.Threshold, rule.DurationSec,
		boolInt(rule.Enabled), string(scope), string(channels), rule.CreatedAt.Unix())
	if err != nil {
		return 0, fmt.Errorf("insert alert rule: %w", err)
	}
	id, _ := res.LastInsertId()
	rule.ID = id
	return id, nil
}

func (r *AlertRepo) UpdateRule(ctx context.Context, rule *model.AlertRule) error {
	scope, _ := json.Marshal(rule.Scope)
	channels, _ := json.Marshal(rule.NotifyChannelIDs)
	res, err := r.db.ExecContext(ctx, `UPDATE alert_rules SET
		name = ?, metric = ?, operator = ?, threshold = ?, duration = ?,
		enabled = ?, scope = ?, notify_channel_ids = ? WHERE id = ?`,
		rule.Name, rule.Metric, rule.Operator, rule.Threshold, rule.DurationSec,
		boolInt(rule.Enabled), string(scope), string(channels), rule.ID)
	if err != nil {
		return fmt.Errorf("update alert rule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAlertNotFound
	}
	return nil
}

func (r *AlertRepo) DeleteRule(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_history WHERE rule_id = ?`, id); err != nil {
		return fmt.Errorf("delete alert history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	return tx.Commit()
}

func scanAlertRule(row rowScanner) (*model.AlertRule, error) {
	var rule model.AlertRule
	var enabled int
	var scope, channels string
	var createdAt int64
	if err := row.Scan(&rule.ID, &rule.Name, &rule.Metric, &rule.Operator, &rule.Threshold,
		&rule.DurationSec, &enabled, &scope, &channels, &createdAt); err != nil {
		return nil, err
	}
	rule.Enabled = enabled != 0
	rule.CreatedAt = time.Unix(createdAt, 0)
	if err := json.Unmarshal([]byte(scope), &rule.Scope); err != nil || rule.Scope.Type == "" {
		rule.Scope = model.AlertScope{Type: "all"}
	}
	_ = json.Unmarshal([]byte(channels), &rule.NotifyChannelIDs)
	return &rule, nil
}

// UpsertHistory moves the latest open row for rule+agent to status, or creates
// one. Resolved rows are never reopened by this method.
func (r *AlertRepo) UpsertHistory(ctx context.Context, ruleID, agentID int64, status string, value float64, at time.Time, notified bool) error {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT id FROM alert_history
		WHERE rule_id = ? AND agent_id = ? AND status != ? ORDER BY id DESC LIMIT 1`,
		ruleID, agentID, model.AlertResolved).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = r.db.ExecContext(ctx, `INSERT INTO alert_history
			(rule_id, agent_id, status, value, started_at, updated_at, notified)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ruleID, agentID, status, value, at.Unix(), at.Unix(), boolInt(notified))
		if err != nil {
			return fmt.Errorf("insert alert history: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("find alert history: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `UPDATE alert_history SET status = ?, value = ?,
		updated_at = ?, notified = ? WHERE id = ?`,
		status, value, at.Unix(), boolInt(notified), id)
	if err != nil {
		return fmt.Errorf("update alert history: %w", err)
	}
	return nil
}

func (r *AlertRepo) ListOpenHistory(ctx context.Context) ([]model.AlertHistory, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, rule_id, agent_id, status, value,
		started_at, updated_at, notified FROM alert_history WHERE status != ? ORDER BY id`,
		model.AlertResolved)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AlertHistory
	for rows.Next() {
		h, err := scanAlertHistory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

func (r *AlertRepo) ListHistory(ctx context.Context, limit int) ([]model.AlertHistory, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, rule_id, agent_id, status, value,
		started_at, updated_at, notified FROM alert_history ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AlertHistory
	for rows.Next() {
		h, err := scanAlertHistory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

func scanAlertHistory(row rowScanner) (*model.AlertHistory, error) {
	var h model.AlertHistory
	var notified int
	var started, updated int64
	if err := row.Scan(&h.ID, &h.RuleID, &h.AgentID, &h.Status, &h.Value,
		&started, &updated, &notified); err != nil {
		return nil, err
	}
	h.StartedAt = time.Unix(started, 0)
	h.UpdatedAt = time.Unix(updated, 0)
	h.Notified = notified != 0
	return &h, nil
}
