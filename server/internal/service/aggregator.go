package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// Aggregator 实时数据聚合落盘(设计文档 5.3):
// 每 5 分钟将环形缓冲窗口聚合为 metric_records 一行; 每日 UTC 聚合前一日为
// metric_records_daily; 同时归档月流量(当月最大累计)并清理超期数据。
type Aggregator struct {
	hub     *Hub
	records *repository.RecordRepo
	agents  *repository.AgentRepo

	retention5m    time.Duration // 默认 90 天
	retentionDaily time.Duration // 默认 365 天

	mu      sync.Mutex
	lastAgg map[int64]int64 // per-agent 上次聚合时间戳水位线, 防窗口重叠重复计数
}

func NewAggregator(hub *Hub, records *repository.RecordRepo, agents *repository.AgentRepo) *Aggregator {
	return &Aggregator{
		hub: hub, records: records, agents: agents,
		retention5m:    90 * 24 * time.Hour,
		retentionDaily: 365 * 24 * time.Hour,
		lastAgg:        map[int64]int64{},
	}
}

// Run 主循环: 每 interval 聚合一次; UTC 日切换时聚合前一日(含宕机跨日回补); 每轮附带清理。
func (a *Aggregator) Run(ctx context.Context, interval time.Duration) {
	// 回补: 从日聚合表最新日期之后到昨日的缺口(审查 MEDIUM #6)
	a.Backfill(ctx, time.Now().UTC())
	lastDay := utcDay(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			now := time.Now()
			if err := a.AggregateOnce(ctx, now); err != nil {
				log.Printf("[aggregator] window: %v", err)
			}
			day := utcDay(now)
			if day != lastDay {
				lastDay = day
				if err := a.AggregateDay(ctx, now.AddDate(0, 0, -1)); err != nil {
					log.Printf("[aggregator] daily: %v", err)
				}
			}
			a.cleanup(ctx, now)
		}
	}
}

// AggregateOnce 聚合当前窗口(每个在线过的 Agent)。
func (a *Aggregator) AggregateOnce(ctx context.Context, now time.Time) error {
	var lastErr error
	for _, id := range a.hub.AgentIDs() {
		reports := a.hub.ReportSnapshot(id)
		if len(reports) == 0 {
			continue
		}
		// 仅聚合自上次水位线之后的新帧, 避免窗口重叠重复计数(审查 MEDIUM #7)
		a.mu.Lock()
		watermark := a.lastAgg[id]
		a.mu.Unlock()
		points := make([]model.Report, 0, len(reports))
		for _, r := range reports {
			if r.Timestamp > watermark && r.Timestamp <= now.Unix() {
				points = append(points, r)
			}
		}
		if len(points) == 0 {
			continue
		}
		mp := Aggregate5m(points, now.Unix())
		a.mu.Lock()
		a.lastAgg[id] = now.Unix()
		a.mu.Unlock()
		if err := a.records.Insert5m(ctx, id, now.Unix(), mp); err != nil {
			lastErr = err
		}
		// 月流量归档(当月最大累计)
		if t := points[len(points)-1].Data.TrafficMonthly; t.Month != "" {
			if err := a.records.UpsertTraffic(ctx, id, t); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

// AggregateDay 将指定日的 5 分钟点聚合为日聚合行(遍历库中全部 Agent, 含已离线)。
func (a *Aggregator) AggregateDay(ctx context.Context, day time.Time) error {
	start := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	date := start.Format("2006-01-02")
	all, err := a.agents.List(ctx)
	if err != nil {
		return err
	}
	for _, agent := range all {
		points, err := a.records.Query5m(ctx, agent.ID, start.Unix(), end.Unix())
		if err != nil {
			return err
		}
		if len(points) == 0 {
			continue
		}
		dp := model.DailyPoint{Date: date, Ping: []model.PingResult{}}
		var cpuSum, memSum, n float64
		for _, p := range points {
			cpuSum += p.CPU
			memSum += p.Mem
			n++
			if p.CPU > dp.CPUMax {
				dp.CPUMax = p.CPU
			}
			if p.Mem > dp.MemMax {
				dp.MemMax = p.Mem
			}
			if p.Rx > dp.Rx {
				dp.Rx = p.Rx
			}
			if p.Tx > dp.Tx {
				dp.Tx = p.Tx
			}
			dp.Ping = mergeWorstPing(dp.Ping, p.Ping)
		}
		dp.CPUAvg = cpuSum / n
		dp.MemAvg = memSum / n
		if err := a.records.InsertDaily(ctx, agent.ID, dp); err != nil {
			return err
		}
	}
	return nil
}

func (a *Aggregator) cleanup(ctx context.Context, now time.Time) {
	if _, err := a.records.Cleanup5m(ctx, now.Add(-a.retention5m).Unix()); err != nil {
		log.Printf("[aggregator] cleanup 5m: %v", err)
	}
	before := utcDay(now.Add(-a.retentionDaily))
	if _, err := a.records.CleanupDaily(ctx, before); err != nil {
		log.Printf("[aggregator] cleanup daily: %v", err)
	}
}

// Backfill 聚合日聚合表缺失的历史日(跨零点宕机/重启后的缺口)。
func (a *Aggregator) Backfill(ctx context.Context, nowUTC time.Time) {
	maxDate, err := a.records.MaxDailyDate(ctx)
	if err != nil {
		log.Printf("[aggregator] backfill: %v", err)
		return
	}
	yesterday := nowUTC.AddDate(0, 0, -1)
	var start time.Time
	if maxDate == "" {
		// 表为空: 无需回补(5m 数据在聚合时自然产生)
		return
	}
	start, err = time.ParseInLocation("2006-01-02", maxDate, time.UTC)
	if err != nil {
		return
	}
	for d := start.AddDate(0, 0, 1); !d.After(yesterday); d = d.AddDate(0, 0, 1) {
		if err := a.AggregateDay(ctx, d); err != nil {
			log.Printf("[aggregator] backfill %s: %v", d.Format("2006-01-02"), err)
			return
		}
		log.Printf("[aggregator] backfilled %s", d.Format("2006-01-02"))
	}
}

// utcDay 返回 UTC 日标记 "2026-08-31"。
func utcDay(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// Aggregate5m 纯聚合函数: CPU/内存/速率取均值, 磁盘按挂载点取均值(设计文档 5.3)。
func Aggregate5m(points []model.Report, ts int64) model.MetricPoint {
	mp := model.MetricPoint{Timestamp: ts, Disk: []model.DiskUsage{}, Ping: []model.PingResult{}}
	var cpuSum, cpuN, memSum, rxSum, txSum float64
	type diskAcc struct {
		total, used float64
		n           int
	}
	disks := map[string]*diskAcc{}
	var n float64
	for _, r := range points {
		n++
		if r.Data.CPU.Usage != nil {
			cpuSum += *r.Data.CPU.Usage
			cpuN++
		}
		if r.Data.Memory.Total > 0 {
			memSum += float64(r.Data.Memory.Used) / float64(r.Data.Memory.Total) * 100
		}
		rxSum += float64(r.Data.Network.RxSpeed)
		txSum += float64(r.Data.Network.TxSpeed)
		for _, d := range r.Data.Disk {
			acc := disks[d.Device]
			if acc == nil {
				acc = &diskAcc{}
				disks[d.Device] = acc
			}
			acc.total += float64(d.Total)
			acc.used += float64(d.Used)
			acc.n++
		}
	}
	if cpuN > 0 {
		mp.CPU = cpuSum / cpuN
	}
	if n > 0 {
		mp.Mem = memSum / n
		mp.Rx = uint64(rxSum / n)
		mp.Tx = uint64(txSum / n)
		for dev, acc := range disks {
			// 按该挂载点的实际样本数取均值, 缺磁盘的帧不稀释
			mp.Disk = append(mp.Disk, model.DiskUsage{Device: dev, Total: uint64(acc.total / float64(acc.n)), Used: uint64(acc.used / float64(acc.n))})
		}
	}
	return mp
}

// mergeWorstPing 按目标合并: avg 取均值, min/max 取极值, loss 取最差(日聚合口径)。
func mergeWorstPing(base []model.PingResult, add []model.PingResult) []model.PingResult {
	out := base
	for _, b := range add {
		found := false
		for i := range out {
			if out[i].Target == b.Target && out[i].IPVersion == b.IPVersion {
				if b.Loss < 100 { // 全丢包哨兵值(60000)不折入日均, 避免拉偏
					out[i].AvgLatency = (out[i].AvgLatency + b.AvgLatency) / 2
				}
				if b.MinLatency < out[i].MinLatency || out[i].MinLatency == 0 {
					out[i].MinLatency = b.MinLatency
				}
				if b.MaxLatency > out[i].MaxLatency {
					out[i].MaxLatency = b.MaxLatency
				}
				if b.Loss > out[i].Loss {
					out[i].Loss = b.Loss
				}
				if b.Loss < 100 {
					out[i].Jitter = (out[i].Jitter + b.Jitter) / 2
				}
				found = true
				break
			}
		}
		if !found {
			c := b
			out = append(out, c)
		}
	}
	return out
}
