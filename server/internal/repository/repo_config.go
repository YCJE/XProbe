package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/YCJE/XProbe/internal/model"
)

// PingTargetRepo 管理探测目标(设计文档 5.6 ping_targets)。
type PingTargetRepo struct {
	db *sql.DB
}

func NewPingTargetRepo(db *sql.DB) *PingTargetRepo { return &PingTargetRepo{db: db} }

// ListEnabled 返回启用的探测目标(Agent 配置拉取用)。
func (r *PingTargetRepo) ListEnabled(ctx context.Context) ([]model.PingTarget, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT target, name, region, isp, ip_version, protocol
		FROM ping_targets WHERE enabled = 1 ORDER BY is_default DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PingTarget
	for rows.Next() {
		var t model.PingTarget
		var region, isp sql.NullString
		if err := rows.Scan(&t.Target, &t.Name, &region, &isp, &t.IPVersion, &t.Protocol); err != nil {
			return nil, err
		}
		t.Region, t.ISP = region.String, isp.String
		out = append(out, t)
	}
	return out, rows.Err()
}

// EnsureSeedDefaults 首次启动写入预置默认目标(is_default=1, 可停用不可删除, 设计文档 5.6)。
// 仅当表为空时写入, 保证开箱即有探测数据且不覆盖管理员改动。
func (r *PingTargetRepo) EnsureSeedDefaults(ctx context.Context, now int64) error {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ping_targets`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	seeds := []model.PingTarget{
		{Target: "114.114.114.114", Name: "电信", ISP: "ct", IPVersion: 4, Protocol: "icmp"},
		{Target: "223.5.5.5", Name: "阿里", ISP: "other", IPVersion: 4, Protocol: "icmp"},
		{Target: "119.29.29.29", Name: "腾讯", ISP: "other", IPVersion: 4, Protocol: "icmp"},
		{Target: "2400:3200::1", Name: "阿里v6", ISP: "other", IPVersion: 6, Protocol: "icmp"},
		{Target: "2001:4860:4860::8888", Name: "Google v6", ISP: "other", IPVersion: 6, Protocol: "icmp"},
	}
	for _, s := range seeds {
		if _, err := r.db.ExecContext(ctx, `INSERT INTO ping_targets
			(target, name, isp, ip_version, protocol, is_default, enabled, created_at)
			VALUES (?, ?, ?, ?, ?, 1, 1, ?)`,
			s.Target, s.Name, s.ISP, s.IPVersion, s.Protocol, now); err != nil {
			return fmt.Errorf("seed ping target %s: %w", s.Target, err)
		}
	}
	return nil
}
