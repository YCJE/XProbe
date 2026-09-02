package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/YCJE/XProbe/internal/model"
)

// ErrNotFound 未找到记录。
var ErrNotFound = errors.New("repository: not found")

// AgentRepo 是 agents 表(含级联数据)的访问层。
type AgentRepo struct {
	db *sql.DB
}

func NewAgentRepo(db *sql.DB) *AgentRepo { return &AgentRepo{db: db} }

const agentCols = `id, token_hash, hostname, display_name, os, arch, agent_version,
	host_fingerprint, ipv4, ipv6, region, country_code, isp, tag_ids,
	expires_at, price_amount, price_currency, price_cycle, traffic_quota_bytes,
	geo_lat, geo_lon, created_at, last_seen, online`

func scanAgent(row interface{ Scan(...any) error }) (*model.Agent, error) {
	var a model.Agent
	var display sql.NullString
	var fp, region, cc, isp, tagIDs sql.NullString
	var priceCurrency, priceCycle sql.NullString
	var expiresAt, quota sql.NullInt64
	var priceAmount sql.NullFloat64
	var geoLat, geoLon sql.NullFloat64
	err := row.Scan(&a.ID, &a.TokenHash, &a.Hostname, &display, &a.OS, &a.Arch, &a.AgentVersion,
		&fp, &a.IPv4, &a.IPv6, &region, &cc, &isp, &tagIDs,
		&expiresAt, &priceAmount, &priceCurrency, &priceCycle, &quota,
		&geoLat, &geoLon, &a.CreatedAt, &a.LastSeen, &a.Online)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.DisplayName = display.String
	a.HostFingerprint = fp.String
	a.Region = region.String
	a.CountryCode = cc.String
	a.ISP = isp.String
	a.TagIDs = tagIDs.String
	a.ExpiresAt = expiresAt.Int64
	a.PriceAmount = priceAmount.Float64
	a.PriceCurrency = priceCurrency.String
	a.PriceCycle = priceCycle.String
	a.TrafficQuotaBytes = quota.Int64
	if geoLat.Valid {
		v := geoLat.Float64
		a.GeoLat = &v
	}
	if geoLon.Valid {
		v := geoLon.Float64
		a.GeoLon = &v
	}
	return &a, nil
}

func (r *AgentRepo) Create(ctx context.Context, a *model.Agent) (int64, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO agents
		(token_hash, hostname, display_name, os, arch, agent_version, host_fingerprint,
		 ipv4, ipv6, created_at, last_seen, online)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.TokenHash, a.Hostname, a.DisplayName, a.OS, a.Arch, a.AgentVersion, a.HostFingerprint,
		a.IPv4, a.IPv6, a.CreatedAt, a.LastSeen, boolInt(a.Online))
	if err != nil {
		return 0, fmt.Errorf("insert agent: %w", err)
	}
	return res.LastInsertId()
}

func (r *AgentRepo) Get(ctx context.Context, id int64) (*model.Agent, error) {
	return scanAgent(r.db.QueryRowContext(ctx,
		`SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

func (r *AgentRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Agent, error) {
	return scanAgent(r.db.QueryRowContext(ctx,
		`SELECT `+agentCols+` FROM agents WHERE token_hash = ?`, tokenHash))
}

func (r *AgentRepo) GetByFingerprint(ctx context.Context, fp string) (*model.Agent, error) {
	return scanAgent(r.db.QueryRowContext(ctx,
		`SELECT `+agentCols+` FROM agents WHERE host_fingerprint = ?`, fp))
}

func (r *AgentRepo) List(ctx context.Context) ([]model.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+agentCols+` FROM agents ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// Touch 更新在线状态与最近活跃时间。
func (r *AgentRepo) Touch(ctx context.Context, id int64, online bool, now int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agents SET online = ?, last_seen = ? WHERE id = ?`, boolInt(online), now, id)
	return err
}

// DeleteCascade 删除 Agent 及其全部级联数据(设计文档 5.1: 删除服务器级联清理)。
func (r *AgentRepo) DeleteCascade(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{
		"metric_records", "metric_records_daily", "traffic_monthly", "alert_history",
	} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE agent_id = ?`, id); err != nil {
			return fmt.Errorf("delete %s: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
