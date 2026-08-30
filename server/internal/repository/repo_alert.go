package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

// NotifyChannelRepo 通知渠道 CRUD(敏感字段回显脱敏由 API 层处理)。
type NotifyChannelRepo struct{ db *sql.DB }

func NewNotifyChannelRepo(db *sql.DB) *NotifyChannelRepo { return &NotifyChannelRepo{db: db} }

func scanChannel(row interface{ Scan(...any) error }) (*model.NotifyChannel, error) {
	var c model.NotifyChannel
	var cfg string
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &cfg); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(cfg), &c.Config)
	return &c, nil
}

func (r *NotifyChannelRepo) List(ctx context.Context) ([]model.NotifyChannel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, type, config FROM notify_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.NotifyChannel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *NotifyChannelRepo) Get(ctx context.Context, id int64) (*model.NotifyChannel, error) {
	c, err := scanChannel(r.db.QueryRowContext(ctx,
		`SELECT id, name, type, config FROM notify_channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (r *NotifyChannelRepo) Create(ctx context.Context, c *model.NotifyChannel) (int64, error) {
	b, _ := json.Marshal(c.Config)
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notify_channels (name, type, config, created_at) VALUES (?, ?, ?, ?)`,
		c.Name, c.Type, string(b), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *NotifyChannelRepo) Update(ctx context.Context, c *model.NotifyChannel) error {
	b, _ := json.Marshal(c.Config)
	res, err := r.db.ExecContext(ctx,
		`UPDATE notify_channels SET name = ?, type = ?, config = ? WHERE id = ?`,
		c.Name, c.Type, string(b), c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *NotifyChannelRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM notify_channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AlertRepo 告警规则与历史(状态机持久化, 设计文档 5.4)。
type AlertRepo struct{ db *sql.DB }

func NewAlertRepo(db *sql.DB) *AlertRepo { return &AlertRepo{db: db} }

const ruleCols = `id, name, metric, operator, threshold, duration, enabled, notify_channel_id`

func scanRule(row interface{ Scan(...any) error }) (*model.AlertRule, error) {
	var r model.AlertRule
	var channel sql.NullInt64
	if err := row.Scan(&r.ID, &r.Name, &r.Metric, &r.Operator, &r.Threshold,
		&r.Duration, &r.Enabled, &channel); err != nil {
		return nil, err
	}
	if channel.Valid {
		r.NotifyChannelID = &channel.Int64
	}
	return &r, nil
}

func (r *AlertRepo) ListRules(ctx context.Context) ([]model.AlertRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+ruleCols+` FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AlertRule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (r *AlertRepo) CreateRule(ctx context.Context, rule *model.AlertRule) (int64, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO alert_rules
		(name, metric, operator, threshold, duration, enabled, notify_channel_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Name, rule.Metric, rule.Operator, rule.Threshold, rule.Duration,
		boolToInt(rule.Enabled), rule.NotifyChannelID, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *AlertRepo) DeleteRule(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertState 维护 (rule, agent) 的未恢复状态行: 无则插入, 有则更新。
func (r *AlertRepo) UpsertState(ctx context.Context, ruleID, agentID int64,
	status string, value *float64, notified bool, now int64) error {
	// 存在未 RESOLVED 行则更新, 否则插入
	res, err := r.db.ExecContext(ctx, `UPDATE alert_history
		SET status = ?, value = ?, notified = ?, updated_at = ?
		WHERE rule_id = ? AND agent_id = ? AND status != 'RESOLVED'`,
		status, value, boolToInt(notified), now, ruleID, agentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO alert_history
		(rule_id, agent_id, status, value, started_at, updated_at, notified)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ruleID, agentID, status, value, now, now, boolToInt(notified))
	return err
}

// LoadOpen 加载未恢复状态(Server 重启后恢复现场, 避免重复通知, 设计文档 10.7)。
func (r *AlertRepo) LoadOpen(ctx context.Context) ([]model.AlertHistory, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, rule_id, agent_id, status, value, started_at, updated_at
		FROM alert_history WHERE status != 'RESOLVED' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AlertHistory{}
	for rows.Next() {
		var h model.AlertHistory
		var value sql.NullFloat64
		if err := rows.Scan(&h.ID, &h.RuleID, &h.AgentID, &h.Status, &value, &h.StartedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		if value.Valid {
			v := value.Float64
			h.Value = &v
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ListHistory 告警时间线(近 limit 条, 含 RESOLVED)。
func (r *AlertRepo) ListHistory(ctx context.Context, limit int) ([]model.AlertHistory, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, rule_id, agent_id, status, value, started_at, updated_at
		FROM alert_history ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AlertHistory{}
	for rows.Next() {
		var h model.AlertHistory
		var value sql.NullFloat64
		if err := rows.Scan(&h.ID, &h.RuleID, &h.AgentID, &h.Status, &value, &h.StartedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		if value.Valid {
			v := value.Float64
			h.Value = &v
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SharePageRepo 公开分享页配置(v1 单条配置)。
type SharePageRepo struct{ db *sql.DB }

func NewSharePageRepo(db *sql.DB) *SharePageRepo { return &SharePageRepo{db: db} }

func (r *SharePageRepo) Get(ctx context.Context) (*model.SharePageConfig, error) {
	var c model.SharePageConfig
	var agentIDs, title, logo, footer sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT share_id, title, logo_url, footer_text, agent_ids
		FROM share_pages ORDER BY id LIMIT 1`).
		Scan(&c.ShareID, &title, &logo, &footer, &agentIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Title, c.LogoURL, c.FooterText = title.String, logo.String, footer.String
	_ = json.Unmarshal([]byte(agentIDs.String), &c.AgentIDs)
	return &c, nil
}

func (r *SharePageRepo) Save(ctx context.Context, c *model.SharePageConfig) error {
	ids, _ := json.Marshal(c.AgentIDs)
	var exists int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM share_pages`).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		_, err := r.db.ExecContext(ctx, `UPDATE share_pages SET
			title = ?, logo_url = ?, footer_text = ?, agent_ids = ? WHERE share_id = ?`,
			c.Title, c.LogoURL, c.FooterText, string(ids), c.ShareID)
		return err
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO share_pages
		(share_id, title, logo_url, footer_text, agent_ids, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.ShareID, c.Title, c.LogoURL, c.FooterText, string(ids), time.Now().Unix())
	return err
}
