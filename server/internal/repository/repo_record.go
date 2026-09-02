package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

// ParseTagIDs 解析 agents.tag_ids JSON 数组("["1,2"]" → [1,2])。
func ParseTagIDs(raw string) []int64 {
	out := []int64{}
	if raw == "" {
		return out
	}
	var arr []int64
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return out
	}
	return arr
}

// RecordRepo 历史聚合数据(5 分钟 + 日聚合, 设计文档 5.3/5.6)。
type RecordRepo struct{ db *sql.DB }

func NewRecordRepo(db *sql.DB) *RecordRepo { return &RecordRepo{db: db} }

// Insert5m 写入 5 分钟聚合点。
func (r *RecordRepo) Insert5m(ctx context.Context, agentID, ts int64, p model.MetricPoint) error {
	disk, _ := json.Marshal(p.Disk)
	ping, _ := json.Marshal(p.Ping)
	_, err := r.db.ExecContext(ctx, `INSERT INTO metric_records
		(agent_id, timestamp, cpu_usage, mem_usage, disk_usage, net_rx, net_tx, ping_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		agentID, ts, p.CPU, p.Mem, string(disk), p.Rx, p.Tx, string(ping))
	return err
}

// InsertDaily 写入日聚合点(UPSERT)。
func (r *RecordRepo) InsertDaily(ctx context.Context, agentID int64, p model.DailyPoint) error {
	disk, _ := json.Marshal(nil)
	ping, _ := json.Marshal(p.Ping)
	_, err := r.db.ExecContext(ctx, `INSERT INTO metric_records_daily
		(agent_id, date, cpu_usage_avg, cpu_usage_max, mem_usage_avg, mem_usage_max,
		 disk_usage, net_rx_avg, net_tx_avg, ping_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id, date) DO UPDATE SET
			cpu_usage_avg = excluded.cpu_usage_avg, cpu_usage_max = excluded.cpu_usage_max,
			mem_usage_avg = excluded.mem_usage_avg, mem_usage_max = excluded.mem_usage_max,
			net_rx_avg = excluded.net_rx_avg, net_tx_avg = excluded.net_tx_avg,
			ping_data = excluded.ping_data`,
		agentID, p.Date, p.CPUAvg, p.CPUMax, p.MemAvg, p.MemMax,
		string(disk), p.Rx, p.Tx, string(ping))
	return err
}

// Query5m 查询 5 分钟聚合点(旧→新)。
func (r *RecordRepo) Query5m(ctx context.Context, agentID, since, until int64) ([]model.MetricPoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT timestamp, COALESCE(cpu_usage,0), COALESCE(mem_usage,0),
		COALESCE(disk_usage,'[]'), COALESCE(net_rx,0), COALESCE(net_tx,0), COALESCE(ping_data,'[]')
		FROM metric_records WHERE agent_id = ? AND timestamp BETWEEN ? AND ? ORDER BY timestamp`,
		agentID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scan5m(rows)
}

func scan5m(rows *sql.Rows) ([]model.MetricPoint, error) {
	out := []model.MetricPoint{}
	for rows.Next() {
		var p model.MetricPoint
		var disk, ping string
		if err := rows.Scan(&p.Timestamp, &p.CPU, &p.Mem, &disk, &p.Rx, &p.Tx, &ping); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(disk), &p.Disk)
		_ = json.Unmarshal([]byte(ping), &p.Ping)
		out = append(out, p)
	}
	return out, rows.Err()
}

// QueryDaily 查询日聚合点(旧→新)。
func (r *RecordRepo) QueryDaily(ctx context.Context, agentID int64, sinceDay, untilDay string) ([]model.DailyPoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT date, COALESCE(cpu_usage_avg,0), COALESCE(cpu_usage_max,0),
		COALESCE(mem_usage_avg,0), COALESCE(mem_usage_max,0), COALESCE(net_rx_avg,0), COALESCE(net_tx_avg,0),
		COALESCE(ping_data,'[]')
		FROM metric_records_daily WHERE agent_id = ? AND date >= ? AND date <= ? ORDER BY date`,
		agentID, sinceDay, untilDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DailyPoint{}
	for rows.Next() {
		var p model.DailyPoint
		var ping string
		if err := rows.Scan(&p.Date, &p.CPUAvg, &p.CPUMax, &p.MemAvg, &p.MemMax, &p.Rx, &p.Tx, &ping); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(ping), &p.Ping)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Cleanup5m 清理超期 5 分钟数据。
func (r *RecordRepo) Cleanup5m(ctx context.Context, before int64) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM metric_records WHERE timestamp < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CleanupDaily 清理超期日聚合数据(审查 HIGH #4: 此前 retentionDaily 无清理调用)。
func (r *RecordRepo) CleanupDaily(ctx context.Context, beforeDay string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM metric_records_daily WHERE date < ?`, beforeDay)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MaxDailyDate 返回日聚合表中最新日期(回补起点, 审查 MEDIUM #6)。
func (r *RecordRepo) MaxDailyDate(ctx context.Context) (string, error) {
	var d sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT MAX(date) FROM metric_records_daily`).Scan(&d)
	if err != nil {
		return "", err
	}
	return d.String, nil // 无数据时 ""
}

// ListAllTraffic 全部 Agent 的月度流量行(报表用, 近 N 月)。
func (r *RecordRepo) ListAllTraffic(ctx context.Context, months int) ([]model.TrafficReportRow, error) {
	cutoff := time.Now().AddDate(0, 0, -months*31).UTC().Format("2006-01")
	rows, err := r.db.QueryContext(ctx, `SELECT t.agent_id,
			COALESCE(a.display_name, a.hostname), t.month, t.rx_bytes, t.tx_bytes
		FROM traffic_monthly t LEFT JOIN agents a ON a.id = t.agent_id
		WHERE t.month >= ? ORDER BY t.month, t.agent_id`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TrafficReportRow{}
	for rows.Next() {
		var r model.TrafficReportRow
		if err := rows.Scan(&r.AgentID, &r.Name, &r.Month, &r.Rx, &r.Tx); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertTraffic 月度流量归档(取当月最大累计值, 设计文档 4.4)。
func (r *RecordRepo) UpsertTraffic(ctx context.Context, agentID int64, t model.TrafficMonthly) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO traffic_monthly (agent_id, month, rx_bytes, tx_bytes)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id, month) DO UPDATE SET
			rx_bytes = MAX(rx_bytes, excluded.rx_bytes),
			tx_bytes = MAX(tx_bytes, excluded.tx_bytes)`,
		agentID, t.Month, t.RxBytes, t.TxBytes)
	return err
}
