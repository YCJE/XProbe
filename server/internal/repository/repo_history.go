package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"time"

	"probe/common/wire"
	"probe/server/internal/model"
)

type MetricSample struct {
	Timestamp int64
	CPU       float64
	Mem       float64
	Disk      float64
	NetRx     float64
	NetTx     float64
}

type PingSample struct {
	Timestamp   int64
	Avg         float64
	Min         float64
	Max         float64
	Jitter      float64
	Loss        float64
	PacketsSent int
	PacketsRecv int
}

type PingSeries struct {
	Target    string
	Name      string
	Method    string
	IPVersion int
	Points    []PingSample
}

type HistoryRepo struct {
	db *sql.DB
}

func NewHistoryRepo(db *sql.DB) *HistoryRepo {
	return &HistoryRepo{db: db}
}

// UpsertMetric folds one report into its five-minute bucket. The unique index
// on (agent_id, timestamp) makes a reconnect or retry idempotent.
func (r *HistoryRepo) UpsertMetric(ctx context.Context, agentID, bucket int64, report wire.Report) error {
	disk := 0.0
	for _, d := range report.Data.Disk {
		if d.Total > 0 {
			if p := float64(d.Used) * 100 / float64(d.Total); p > disk {
				disk = p
			}
		}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO metric_records
		(agent_id, timestamp, cpu_usage, mem_usage, disk_usage, net_rx, net_tx, ping_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, '')
		ON CONFLICT(agent_id, timestamp) DO UPDATE SET
			cpu_usage = excluded.cpu_usage,
			mem_usage = excluded.mem_usage,
			disk_usage = excluded.disk_usage,
			net_rx = excluded.net_rx,
			net_tx = excluded.net_tx`,
		agentID, bucket, report.Data.CPU.Usage,
		percent(report.Data.Memory.Used, report.Data.Memory.Total),
		strconv.FormatFloat(disk, 'f', 2, 64),
		int64(report.Data.Network.RxSpeed), int64(report.Data.Network.TxSpeed))
	if err != nil {
		return fmt.Errorf("upsert metric: %w", err)
	}
	return nil
}

func (r *HistoryRepo) InsertPings(ctx context.Context, agentID int64, stats []wire.PingStat, at time.Time) error {
	if len(stats) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ping insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO ping_records
		(agent_id, target, name, method, ip_version, timestamp, avg_latency,
		 min_latency, max_latency, jitter, loss, packets_sent, packets_recv)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare ping insert: %w", err)
	}
	defer stmt.Close()
	for _, s := range stats {
		if _, err := stmt.ExecContext(ctx, agentID, s.Target, s.Name, s.Method, s.IPVersion,
			at.Unix(), s.AvgLatency, s.MinLatency, s.MaxLatency, s.Jitter, s.Loss,
			s.PacketsSent, s.PacketsRecv); err != nil {
			return fmt.Errorf("insert ping %q: %w", s.Target, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ping insert: %w", err)
	}
	return nil
}

func (r *HistoryRepo) ListMetrics(ctx context.Context, agentID int64, since time.Time) ([]MetricSample, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT timestamp, COALESCE(cpu_usage,0),
		COALESCE(mem_usage,0), COALESCE(disk_usage,'0'), COALESCE(net_rx,0), COALESCE(net_tx,0)
		FROM metric_records WHERE agent_id = ? AND timestamp >= ? ORDER BY timestamp`,
		agentID, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("list metrics: %w", err)
	}
	defer rows.Close()
	var out []MetricSample
	for rows.Next() {
		var s MetricSample
		var disk string
		if err := rows.Scan(&s.Timestamp, &s.CPU, &s.Mem, &disk, &s.NetRx, &s.NetTx); err != nil {
			return nil, fmt.Errorf("scan metric: %w", err)
		}
		s.Disk, _ = strconv.ParseFloat(disk, 64)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListPings groups samples by target and caps each series at maxPerSeries by
// uniform downsampling, always keeping the newest point.
func (r *HistoryRepo) ListPings(ctx context.Context, agentID int64, since time.Time, maxPerSeries int) ([]PingSeries, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT target, name, method, ip_version,
		timestamp, avg_latency, min_latency, max_latency, jitter, loss, packets_sent, packets_recv
		FROM ping_records WHERE agent_id = ? AND timestamp >= ? ORDER BY target, timestamp`,
		agentID, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("list pings: %w", err)
	}
	defer rows.Close()
	byTarget := map[string]*PingSeries{}
	for rows.Next() {
		var target, name, method string
		var ipVersion, sent, recv int
		var ts int64
		var avg, minV, maxV, jitter, loss float64
		if err := rows.Scan(&target, &name, &method, &ipVersion, &ts,
			&avg, &minV, &maxV, &jitter, &loss, &sent, &recv); err != nil {
			return nil, fmt.Errorf("scan ping: %w", err)
		}
		s := byTarget[target]
		if s == nil {
			s = &PingSeries{Target: target, Name: name, Method: method, IPVersion: ipVersion}
			byTarget[target] = s
		}
		s.Name, s.Method, s.IPVersion = name, method, ipVersion
		s.Points = append(s.Points, PingSample{
			Timestamp: ts, Avg: avg, Min: minV, Max: maxV, Jitter: jitter,
			Loss: loss, PacketsSent: sent, PacketsRecv: recv,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]PingSeries, 0, len(byTarget))
	for _, s := range byTarget {
		s.Points = downsample(s.Points, maxPerSeries)
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out, nil
}

func downsample(points []PingSample, max int) []PingSample {
	if max <= 0 || len(points) <= max {
		return points
	}
	every := (len(points) + max - 1) / max
	out := make([]PingSample, 0, max)
	for i := 0; i < len(points); i += every {
		out = append(out, points[i])
	}
	if len(out) == 0 || out[len(out)-1].Timestamp != points[len(points)-1].Timestamp {
		out = append(out, points[len(points)-1])
	}
	return out
}

func (r *HistoryRepo) Cleanup(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	cleanups := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM metric_records WHERE timestamp < ?`, []any{cutoff}},
		{`DELETE FROM ping_records WHERE timestamp < ?`, []any{cutoff}},
		// Keep unresolved alerts regardless of age: deleting a still-firing
		// row would orphan the engine's open state and lose the resolution
		// record when it eventually closes.
		{`DELETE FROM alert_history WHERE status = ? AND updated_at < ?`, []any{model.AlertResolved, cutoff}},
		{`DELETE FROM audit_log WHERE timestamp < ?`, []any{cutoff}},
	}
	for _, cleanup := range cleanups {
		if _, err := r.db.ExecContext(ctx, cleanup.query, cleanup.args...); err != nil {
			return fmt.Errorf("cleanup history: %w", err)
		}
	}
	return nil
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

type AuditRepo struct {
	db *sql.DB
}

func NewAuditRepo(db *sql.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

func (r *AuditRepo) Write(ctx context.Context, at time.Time, actor, action, target, detail string) error {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO audit_log
		(timestamp, actor, action, target, detail) VALUES (?, ?, ?, ?, ?)`,
		at.Unix(), actor, action, target, detail); err != nil {
		return fmt.Errorf("write audit: %w", err)
	}
	return nil
}

func (r *AuditRepo) List(ctx context.Context, limit int) ([]model.AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, timestamp, actor, action, target, detail
		FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()
	var out []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.Action, &e.Target, &e.Detail); err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
