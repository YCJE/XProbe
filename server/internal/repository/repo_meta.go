package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

// TagRepo 彩色标签 CRUD。
type TagRepo struct{ db *sql.DB }

func NewTagRepo(db *sql.DB) *TagRepo { return &TagRepo{db: db} }

func (r *TagRepo) List(ctx context.Context) ([]model.Tag, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, COALESCE(color,'') FROM tags ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Tag{}
	for rows.Next() {
		var t model.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TagRepo) Create(ctx context.Context, t model.Tag) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tags (name, color, created_at) VALUES (?, ?, ?)`,
		t.Name, t.Color, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *TagRepo) Update(ctx context.Context, t model.Tag) error {
	res, err := r.db.ExecContext(ctx, `UPDATE tags SET name = ?, color = ? WHERE id = ?`,
		t.Name, t.Color, t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *TagRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMeta 更新服务器元数据(设计文档 5.8; Agent 无法上报覆盖)。
func (r *AgentRepo) UpdateMeta(ctx context.Context, id int64, m model.UpdateMetaRequest) error {
	tags := "[]"
	if len(m.TagIDs) > 0 {
		parts := make([]string, len(m.TagIDs))
		for i, v := range m.TagIDs {
			parts[i] = fmt.Sprintf("%d", v)
		}
		tags = "[" + strings.Join(parts, ",") + "]"
	}
	res, err := r.db.ExecContext(ctx, `UPDATE agents SET
		display_name = ?, region = ?, country_code = ?, isp = ?, tag_ids = ?,
		expires_at = ?, price_amount = ?, price_currency = ?, price_cycle = ?, traffic_quota_bytes = ?,
		geo_lat = ?, geo_lon = ?
		WHERE id = ?`,
		m.DisplayName, m.Region, m.CountryCode, m.ISP, tags,
		m.ExpiresAt, m.PriceAmount, m.PriceCurrency, m.PriceCycle, m.TrafficQuotaBytes,
		m.GeoLat, m.GeoLon, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetFingerprint 清空指纹绑定(设计文档 7.5: Agent 重新注册时重新绑定)。
func (r *AgentRepo) ResetFingerprint(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET host_fingerprint = NULL WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateBind 将预创建节点与 Agent 注册信息绑定(Komari 模式)。
func (r *AgentRepo) UpdateBind(ctx context.Context, id int64, a *model.Agent) error {
	_, err := r.db.ExecContext(ctx, `UPDATE agents SET
		token_hash = ?, hostname = ?, os = ?, arch = ?, agent_version = ?,
		host_fingerprint = ?, ipv4 = ?, last_seen = ?, online = 1
		WHERE id = ?`,
		a.TokenHash, a.Hostname, a.OS, a.Arch, a.AgentVersion,
		a.HostFingerprint, a.IPv4, a.LastSeen, id)
	return err
}

// RotateToken 吊销旧 Token 并写入新 Token 哈希(设计文档 7.5 v1.3)。
func (r *AgentRepo) RotateToken(ctx context.Context, id int64, newTokenHash string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET token_hash = ? WHERE id = ?`, newTokenHash, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTraffic 月度流量归档(近 N 个月, 倒序)。
func (r *AgentRepo) ListTraffic(ctx context.Context, agentID int64, limit int) ([]model.TrafficMonthly, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT month, rx_bytes, tx_bytes FROM traffic_monthly
		WHERE agent_id = ? ORDER BY month DESC LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TrafficMonthly{}
	for rows.Next() {
		var t model.TrafficMonthly
		if err := rows.Scan(&t.Month, &t.RxBytes, &t.TxBytes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// maskToken 供列表展示: 前 8 位 + 掩码。
func MaskToken(h string) string {
	if len(h) <= 8 {
		return "***"
	}
	return h[:8] + "…"
}
