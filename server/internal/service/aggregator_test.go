package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

func u(v float64) *float64 { return &v }

func TestAggregate5m(t *testing.T) {
	points := []model.Report{
		{Timestamp: 1, Data: model.ReportData{
			CPU: model.CPUInfo{Usage: u(40)}, Memory: model.MemoryInfo{Total: 100, Used: 50},
			Network: model.NetworkInfo{RxSpeed: 1000, TxSpeed: 500},
			Disk:    []model.DiskUsage{{Device: "/", Total: 100, Used: 40}},
		}},
		{Timestamp: 2, Data: model.ReportData{
			CPU: model.CPUInfo{Usage: u(60)}, Memory: model.MemoryInfo{Total: 100, Used: 70},
			Network: model.NetworkInfo{RxSpeed: 2000, TxSpeed: 1000},
			Disk:    []model.DiskUsage{{Device: "/", Total: 100, Used: 50}},
		}},
		{Timestamp: 3, Data: model.ReportData{ // 首采样 nil 不计入 CPU 均值
			Memory: model.MemoryInfo{Total: 100, Used: 90},
		}},
	}
	mp := Aggregate5m(points, 99)
	if mp.Timestamp != 99 {
		t.Fatalf("ts = %d", mp.Timestamp)
	}
	if mp.CPU != 50 { // (40+60)/2, nil 不计入
		t.Fatalf("cpu = %v", mp.CPU)
	}
	if mp.Mem != 70 { // (50+70+90)/3
		t.Fatalf("mem = %v", mp.Mem)
	}
	if mp.Rx != 1000 || mp.Tx != 500 { // (1000+2000+0)/3: 零速率也是真实样本
		t.Fatalf("rx/tx = %d/%d", mp.Rx, mp.Tx)
	}
	if len(mp.Disk) != 1 || mp.Disk[0].Device != "/" || mp.Disk[0].Used != 45 {
		t.Fatalf("disk = %+v", mp.Disk)
	}
}

func TestMergeWorstPing(t *testing.T) {
	base := []model.PingResult{{Target: "a", Name: "电信", IPVersion: 4, AvgLatency: 10, MinLatency: 8, MaxLatency: 12, Loss: 0, Jitter: 1}}
	add := []model.PingResult{
		{Target: "a", Name: "电信", IPVersion: 4, AvgLatency: 20, MinLatency: 9, MaxLatency: 40, Loss: 5, Jitter: 3},
		{Target: "b", Name: "联通", IPVersion: 4, AvgLatency: 30, MinLatency: 28, MaxLatency: 32, Loss: 0, Jitter: 2},
	}
	out := mergeWorstPing(base, add)
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	a := out[0]
	if a.AvgLatency != 15 || a.MinLatency != 8 || a.MaxLatency != 40 || a.Loss != 5 || a.Jitter != 2 {
		t.Fatalf("merged = %+v", a)
	}
}

func TestAggregator_DayAggregate(t *testing.T) {
	db, err := repository.Open(filepath.Join(t.TempDir(), "agg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	agents := repository.NewAgentRepo(db)
	records := repository.NewRecordRepo(db)
	hub := NewHub(agents, 90*time.Second)

	agentID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "h", Hostname: "a", HostFingerprint: "f", CreatedAt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	start := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	// 灌入两行 5 分钟点
	_ = records.Insert5m(ctx, agentID, start.Add(time.Hour).Unix(),
		model.MetricPoint{CPU: 30, Mem: 40})
	_ = records.Insert5m(ctx, agentID, start.Add(2*time.Hour).Unix(),
		model.MetricPoint{CPU: 70, Mem: 60,
			Ping: []model.PingResult{{Target: "114.114.114.114", Name: "电信", IPVersion: 4, AvgLatency: 20, Loss: 10}}})

	agg := NewAggregator(hub, records, agents)
	if err := agg.AggregateDay(ctx, day); err != nil {
		t.Fatalf("AggregateDay: %v", err)
	}

	daily, err := records.QueryDaily(ctx, agentID, "2026-08-30", "2026-08-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 1 {
		t.Fatalf("daily rows = %d", len(daily))
	}
	d := daily[0]
	if d.Date != "2026-08-30" || d.CPUAvg != 50 || d.CPUMax != 70 || d.MemAvg != 50 || d.MemMax != 60 {
		t.Fatalf("daily = %+v", d)
	}
	if len(d.Ping) != 1 || d.Ping[0].Loss != 10 {
		t.Fatalf("daily ping = %+v", d.Ping)
	}

	// 聚合月流量 upsert
	if err := records.UpsertTraffic(ctx, agentID, model.TrafficMonthly{Month: "2026-08", RxBytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := records.UpsertTraffic(ctx, agentID, model.TrafficMonthly{Month: "2026-08", RxBytes: 300}); err != nil {
		t.Fatal(err)
	}
	traffic, _ := agents.ListTraffic(ctx, agentID, 12)
	if len(traffic) != 1 || traffic[0].RxBytes != 300 {
		t.Fatalf("traffic = %+v, want max(100,300)=300", traffic)
	}
}
