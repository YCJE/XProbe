package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

type fakeSender struct {
	mu     sync.Mutex
	events []string // "status:channelID"
}

func (f *fakeSender) Send(_ context.Context, channelID int64, title, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, title)
	_ = channelID
	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

func newEngineFixture(t *testing.T) (*AlertEngine, *Hub, *repository.AgentRepo, *repository.AlertRepo, *fakeSender, *timeControl) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "alert.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	agents := repository.NewAgentRepo(db)
	rules := repository.NewAlertRepo(db)
	hub := NewHub(agents, 90*time.Second)
	sender := &fakeSender{}
	tc := &timeControl{now: time.Now()}
	engine := NewAlertEngine(rules, agents, hub, sender)
	engine.now = tc.get
	hub.SetClock(tc.get)
	return engine, hub, agents, rules, sender, tc
}

func waitUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

type timeControl struct {
	mu  sync.Mutex
	now time.Time
}

func (t *timeControl) get() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now
}

func (t *timeControl) advance(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = t.now.Add(d)
}

func TestAlertEngine_StateMachine(t *testing.T) {
	engine, hub, agents, rules, sender, tc := newEngineFixture(t)
	ctx := context.Background()

	chID := int64(1)
	agentID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "h", Hostname: "web-01", HostFingerprint: "f", CreatedAt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	cpu := 90.0
	if _, err := rules.CreateRule(ctx, &model.AlertRule{
		Name: "CPU高", Metric: model.MetricCPU, Operator: ">", Threshold: 80,
		Duration: 300, Enabled: true, NotifyChannelID: &chID,
	}); err != nil {
		t.Fatal(err)
	}

	// Agent 在线 + CPU 90%
	fc := &fakeConn{}
	if err := hub.Attach(agentID, fc); err != nil {
		t.Fatal(err)
	}
	if err := hub.HandleReport(agentID, &model.Report{
		Type: model.FrameReport, Timestamp: tc.get().Unix(), Hostname: "web-01",
		Data: model.ReportData{CPU: model.CPUInfo{Usage: &cpu}, Memory: model.MemoryInfo{Total: 100, Used: 50}},
	}); err != nil {
		t.Fatalf("report: %v", err)
	}

	// 第一次评估: 超阈值但未达 duration → PENDING, 不通知
	if err := engine.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 0 {
		t.Fatalf("PENDING should not notify, got %d", sender.count())
	}
	open, _ := rules.LoadOpen(ctx)
	if len(open) != 1 || open[0].Status != StatusPending {
		t.Fatalf("open = %+v, want PENDING", open)
	}

	// 时间前进 6 分钟(> 300s) → FIRING + 通知
	tc.advance(6 * time.Minute)
	hub.SetClock(tc.get)
	if err := hub.HandleReport(agentID, &model.Report{
		Type: model.FrameReport, Timestamp: tc.get().Unix(), Hostname: "web-01",
		Data: model.ReportData{CPU: model.CPUInfo{Usage: &cpu}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if !waitUntil(2*time.Second, func() bool { return sender.count() >= 1 }) {
		t.Fatal("FIRING should notify once")
	}

	// 静默期内(60min)未恢复: 重复评估不重复通知
	tc.advance(5 * time.Minute)
	hub.SetClock(tc.get)
	if err := engine.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 {
		t.Fatalf("silence should suppress, got %d", sender.count())
	}

	// 恢复: CPU 降到 50 → RESOLVED + 恢复通知
	low := 50.0
	hub.SetClock(tc.get)
	if err := hub.HandleReport(agentID, &model.Report{
		Type: model.FrameReport, Timestamp: tc.get().Unix(), Hostname: "web-01",
		Data: model.ReportData{CPU: model.CPUInfo{Usage: &low}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if !waitUntil(2*time.Second, func() bool { return sender.count() >= 2 }) {
		t.Fatal("RESOLVED should notify recovery")
	}
	open, _ = rules.LoadOpen(ctx)
	if len(open) != 0 {
		t.Fatalf("no open state after resolve, got %d", len(open))
	}
	history, _ := rules.ListHistory(ctx, 10)
	if len(history) == 0 || history[0].Status != StatusResolved {
		t.Fatalf("history = %+v", history)
	}
}

func TestAlertEngine_OfflineImmediateFiring(t *testing.T) {
	engine, _, agents, rules, sender, _ := newEngineFixture(t)
	ctx := context.Background()

	chID := int64(1)
	agentID, _ := agents.Create(ctx, &model.Agent{
		TokenHash: "h", Hostname: "a", HostFingerprint: "f", CreatedAt: 1,
	})
	if _, err := rules.CreateRule(ctx, &model.AlertRule{
		Name: "离线", Metric: model.MetricOffline, Operator: "=", Threshold: 1,
		Duration: 0, Enabled: true, NotifyChannelID: &chID,
	}); err != nil {
		t.Fatal(err)
	}

	// 离线事件 → 即时 FIRING(agent_offline 事件驱动, 设计文档 5.4)
	engine.HandleOffline(ctx, agentID)
	if !waitUntil(2*time.Second, func() bool { return sender.count() >= 1 }) {
		t.Fatal("offline should notify immediately")
	}
	// 上线 → RESOLVED
	engine.HandleOnline(ctx, agentID)
	if !waitUntil(2*time.Second, func() bool { return sender.count() >= 2 }) {
		t.Fatal("online should notify recovery")
	}
	open, _ := rules.LoadOpen(ctx)
	if len(open) != 0 {
		t.Fatalf("open = %d, want 0", len(open))
	}
}

func TestAlertEngine_RestoreAfterRestart(t *testing.T) {
	engine, hub, agents, rules, sender, tc := newEngineFixture(t)
	ctx := context.Background()

	chID := int64(1)
	agentID, _ := agents.Create(ctx, &model.Agent{
		TokenHash: "h", Hostname: "a", HostFingerprint: "f", CreatedAt: 1,
	})
	cpu := 90.0
	if _, err := rules.CreateRule(ctx, &model.AlertRule{
		Name: "CPU", Metric: model.MetricCPU, Operator: ">", Threshold: 80,
		Duration: 300, Enabled: true, NotifyChannelID: &chID,
	}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeConn{}
	_ = hub.Attach(agentID, fc)
	if err := hub.HandleReport(agentID, &model.Report{
		Type: model.FrameReport, Timestamp: tc.get().Unix(), Hostname: "a",
		Data: model.ReportData{CPU: model.CPUInfo{Usage: &cpu}},
	}); err != nil {
		t.Fatal(err)
	}
	// 先 PENDING
	if err := engine.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	// 再 FIRING
	tc.advance(6 * time.Minute)
	hub.SetClock(tc.get)
	if err := hub.HandleReport(agentID, &model.Report{
		Type: model.FrameReport, Timestamp: tc.get().Unix(), Hostname: "a",
		Data: model.ReportData{CPU: model.CPUInfo{Usage: &cpu}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if !waitUntil(2*time.Second, func() bool { return sender.count() >= 1 }) {
		t.Fatal("firing notify")
	}

	// 模拟重启: 新引擎 Restore → FIRING 状态恢复, 不重复通知(设计文档 10.7)
	engine2 := NewAlertEngine(rules, agents, hub, sender)
	engine2.now = tc.get
	if err := engine2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engine2.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 {
		t.Fatalf("after restore should not re-notify within silence, got %d", sender.count())
	}
}

func TestAlertEngine_DeadlockRegression_MultiAgentNoState(t *testing.T) {
	// 审查 CRITICAL #1 回归: !met 且无状态的路径曾持锁 return, 多 Agent 下第二次
	// Evaluate 即自死锁。本测试若死锁将在超时后失败。
	engine, hub, agents, rules, _, tc := newEngineFixture(t)
	ctx := context.Background()

	chID := int64(1)
	// 两个 Agent: 一个高 CPU 满足规则, 一个无报告不满足(常态路径)
	highID, _ := agents.Create(ctx, &model.Agent{TokenHash: "h1", Hostname: "a1", HostFingerprint: "f1", CreatedAt: 1})
	lowID, _ := agents.Create(ctx, &model.Agent{TokenHash: "h2", Hostname: "a2", HostFingerprint: "f2", CreatedAt: 1})
	if _, err := rules.CreateRule(ctx, &model.AlertRule{
		Name: "CPU", Metric: model.MetricCPU, Operator: ">", Threshold: 80,
		Duration: 60, Enabled: true, NotifyChannelID: &chID,
	}); err != nil {
		t.Fatal(err)
	}

	cpu := 90.0
	if err := hub.Attach(highID, &fakeConn{}); err != nil {
		t.Fatal(err)
	}
	if err := hub.HandleReport(highID, &model.Report{
		Type: model.FrameReport, Timestamp: tc.get().Unix(), Hostname: "a1",
		Data: model.ReportData{CPU: model.CPUInfo{Usage: &cpu}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := hub.Attach(lowID, &fakeConn{}); err != nil {
		t.Fatal(err)
	}

	runEval := func(name string) {
		done := make(chan struct{})
		go func() {
			_ = engine.Evaluate(ctx) // 若死锁则永远阻塞
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s deadlocked", name)
		}
	}
	runEval("Evaluate on !met && no-state path")
	runEval("second Evaluate")
}
