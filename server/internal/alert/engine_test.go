package alert

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"probe/common/wire"
	"probe/server/internal/model"
	"probe/server/internal/monitor"
	"probe/server/internal/repository"
)

type fakeNotifier struct{ sent []string }

func (f *fakeNotifier) Send(ctx context.Context, channel model.NotifyChannel, title, message string) error {
	f.sent = append(f.sent, title)
	return nil
}

func TestAlertEngineFiresAndResolves(t *testing.T) {
	ctx := context.Background()
	db, err := repository.Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	agents := repository.NewAgentRepo(db)
	agentID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "hash", Hostname: "web-01", HostFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatal(err)
	}
	notifyRepo := repository.NewNotifyRepo(db)
	channelID, err := notifyRepo.Create(ctx, &model.NotifyChannel{
		Name: "fake", Type: "webhook", Config: `{}`,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	alertRepo := repository.NewAlertRepo(db)
	rule := &model.AlertRule{
		Name: "CPU", Metric: "cpu", Operator: ">", Threshold: 50, DurationSec: 0,
		Enabled: true, Scope: model.AlertScope{Type: "all"}, NotifyChannelIDs: []int64{channelID},
		CreatedAt: time.Now(),
	}
	if _, err := alertRepo.CreateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}

	hub := monitor.NewHub()
	hub.Attach(agentID, "test-conn", func() {})
	notifier := &fakeNotifier{}
	engine := NewEngine(alertRepo, agents, notifyRepo, hub, notifier,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	engine.now = func() time.Time { return now }

	hub.Store(agentID, wire.Report{Type: wire.TypeReport, Hostname: "web-01",
		Data: wire.ReportData{CPU: wire.CPUMetrics{Usage: 80}}})
	engine.Tick(ctx) // PENDING
	now = now.Add(time.Second)
	engine.Tick(ctx) // FIRING + notify
	history, err := alertRepo.ListHistory(ctx, 10)
	if err != nil || len(history) != 1 || history[0].Status != model.AlertFiring {
		t.Fatalf("history after fire: %+v %v", history, err)
	}
	if len(notifier.sent) != 1 {
		t.Fatalf("notifications = %v", notifier.sent)
	}

	// A stale last report must not keep the CPU alert firing after the agent
	// disconnects.
	hub.Detach(agentID, "test-conn")
	now = now.Add(time.Second)
	engine.Tick(ctx) // RESOLVED + recovery notify
	history, err = alertRepo.ListHistory(ctx, 10)
	if err != nil || len(history) != 1 || history[0].Status != model.AlertResolved {
		t.Fatalf("history after resolve: %+v %v", history, err)
	}
	if len(notifier.sent) != 2 {
		t.Fatalf("notifications after resolve = %v", notifier.sent)
	}
}

func TestAlertScopeAndOffline(t *testing.T) {
	agent := model.Agent{ID: 2, TagIDs: "[3,4]"}
	if !inScope(model.AlertScope{Type: "all"}, agent) {
		t.Fatal("all scope rejected")
	}
	if inScope(model.AlertScope{Type: "agent_ids", IDs: []int64{1}}, agent) {
		t.Fatal("wrong agent id accepted")
	}
	if !inScope(model.AlertScope{Type: "tag_ids", IDs: []int64{4}}, agent) {
		t.Fatal("tag scope rejected")
	}
	if evaluateCondition(model.AlertRule{Metric: "offline"}, 0, agent, false) != true {
		t.Fatal("offline condition did not trigger")
	}
}

func TestLoadStateKeepsNotificationRetry(t *testing.T) {
	ctx := context.Background()
	db, err := repository.Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	agents := repository.NewAgentRepo(db)
	agentID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "retry-hash", Hostname: "retry-01",
		HostFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatal(err)
	}
	alertRepo := repository.NewAlertRepo(db)
	rule := &model.AlertRule{
		Name: "CPU", Metric: "cpu", Operator: ">", Threshold: 50,
		Enabled: true, Scope: model.AlertScope{Type: "all"}, CreatedAt: time.Now(),
	}
	if _, err := alertRepo.CreateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	// A firing row whose notification never succeeded must be retried after
	// a restart, so loadState must preserve notified=false.
	if err := alertRepo.UpsertHistory(ctx, rule.ID, agentID, model.AlertFiring, 80, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(alertRepo, agents, repository.NewNotifyRepo(db), monitor.NewHub(),
		&fakeNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.loadState(ctx)
	engine.mu.Lock()
	st := engine.state[key(rule.ID, agentID)]
	engine.mu.Unlock()
	if st == nil {
		t.Fatal("open firing alert was not restored")
	}
	if st.notified {
		t.Fatal("failed notification was treated as delivered after restart")
	}
}

func TestScopeNarrowingResolvesOpenAlert(t *testing.T) {
	ctx := context.Background()
	db, err := repository.Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	agents := repository.NewAgentRepo(db)
	agentID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "scope-hash", Hostname: "scope-01",
		HostFingerprint: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	})
	if err != nil {
		t.Fatal(err)
	}
	alertRepo := repository.NewAlertRepo(db)
	rule := &model.AlertRule{
		Name: "CPU", Metric: "cpu", Operator: ">", Threshold: 50,
		Enabled: true, Scope: model.AlertScope{Type: "all"}, CreatedAt: time.Now(),
	}
	if _, err := alertRepo.CreateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	hub := monitor.NewHub()
	hub.Attach(agentID, "scope-conn", func() {})
	hub.Store(agentID, wire.Report{Type: wire.TypeReport, Hostname: "scope-01",
		Data: wire.ReportData{CPU: wire.CPUMetrics{Usage: 80}}})
	engine := NewEngine(alertRepo, agents, repository.NewNotifyRepo(db), hub,
		&fakeNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	engine.now = func() time.Time { return now }
	engine.Tick(ctx)
	now = now.Add(time.Second)
	engine.Tick(ctx)
	history, err := alertRepo.ListHistory(ctx, 10)
	if err != nil || len(history) != 1 || history[0].Status != model.AlertFiring {
		t.Fatalf("history before scope change: %+v %v", history, err)
	}

	rule.Scope = model.AlertScope{Type: "agent_ids", IDs: []int64{agentID + 100}}
	if err := alertRepo.UpdateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	engine.Tick(ctx)
	history, err = alertRepo.ListHistory(ctx, 10)
	if err != nil || len(history) != 1 || history[0].Status != model.AlertResolved {
		t.Fatalf("alert stayed open after scope change: %+v %v", history, err)
	}
}

type flakyNotifier struct {
	attempts int
	fail     int
	sent     []string
}

func (f *flakyNotifier) Send(ctx context.Context, channel model.NotifyChannel, title, message string) error {
	f.attempts++
	if f.attempts <= f.fail {
		return errors.New("notification endpoint down")
	}
	f.sent = append(f.sent, title)
	return nil
}

func TestFiringNotificationRetries(t *testing.T) {
	ctx := context.Background()
	db, err := repository.Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	agents := repository.NewAgentRepo(db)
	agentID, err := agents.Create(ctx, &model.Agent{
		TokenHash: "retry-send", Hostname: "retry-send-01",
		HostFingerprint: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	})
	if err != nil {
		t.Fatal(err)
	}
	notifyRepo := repository.NewNotifyRepo(db)
	channelID, err := notifyRepo.Create(ctx, &model.NotifyChannel{
		Name: "flaky", Type: "webhook", Config: `{}`,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	alertRepo := repository.NewAlertRepo(db)
	rule := &model.AlertRule{
		Name: "CPU", Metric: "cpu", Operator: ">", Threshold: 50, DurationSec: 0,
		Enabled: true, Scope: model.AlertScope{Type: "all"},
		NotifyChannelIDs: []int64{channelID}, CreatedAt: time.Now(),
	}
	if _, err := alertRepo.CreateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	hub := monitor.NewHub()
	hub.Attach(agentID, "retry-conn", func() {})
	hub.Store(agentID, wire.Report{Type: wire.TypeReport, Hostname: "retry-send-01",
		Data: wire.ReportData{CPU: wire.CPUMetrics{Usage: 80}}})
	notifier := &flakyNotifier{fail: 1}
	engine := NewEngine(alertRepo, agents, notifyRepo, hub, notifier,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	engine.now = func() time.Time { return now }
	engine.Tick(ctx) // PENDING
	now = now.Add(time.Second)
	engine.Tick(ctx) // FIRING, first delivery fails
	if notifier.attempts != 1 || len(notifier.sent) != 0 {
		t.Fatalf("first delivery: attempts=%d sent=%v", notifier.attempts, notifier.sent)
	}
	now = now.Add(6 * time.Minute)
	engine.Tick(ctx) // retry while the condition remains true
	if notifier.attempts != 2 || len(notifier.sent) != 1 {
		t.Fatalf("retry: attempts=%d sent=%v", notifier.attempts, notifier.sent)
	}
	history, err := alertRepo.ListHistory(ctx, 10)
	if err != nil || len(history) != 1 || !history[0].Notified {
		t.Fatalf("history not marked notified after retry: %+v %v", history, err)
	}
}
