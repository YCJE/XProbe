// Package alert evaluates threshold rules against the latest agent reports.
// It is intentionally small: one process, one admin, a handful of rules.
package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"probe/common/wire"
	"probe/server/internal/model"
	"probe/server/internal/monitor"
	"probe/server/internal/repository"
)

type Notifier interface {
	Send(ctx context.Context, channel model.NotifyChannel, title, message string) error
}

type Engine struct {
	alerts     *repository.AlertRepo
	agents     *repository.AgentRepo
	notifyRepo *repository.NotifyRepo
	hub        *monitor.Hub
	notifier   Notifier
	logger     *slog.Logger
	now        func() time.Time

	mu       sync.Mutex
	state    map[string]*ruleState
	rules    map[int64]model.AlertRule
	channels map[int64]model.NotifyChannel
}

type ruleState struct {
	status      string
	since       time.Time
	notified    bool
	lastAttempt time.Time
}

func NewEngine(alerts *repository.AlertRepo, agents *repository.AgentRepo,
	notifyRepo *repository.NotifyRepo, hub *monitor.Hub, notifier Notifier, logger *slog.Logger) *Engine {
	return &Engine{
		alerts: alerts, agents: agents, notifyRepo: notifyRepo, hub: hub,
		notifier: notifier, logger: logger,
		now: time.Now, state: map[string]*ruleState{},
		rules: map[int64]model.AlertRule{}, channels: map[int64]model.NotifyChannel{},
	}
}

func (e *Engine) Run(ctx context.Context) {
	e.loadState(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.Tick(ctx)
		}
	}
}

func (e *Engine) loadState(ctx context.Context) {
	open, err := e.alerts.ListOpenHistory(ctx)
	if err != nil {
		e.logger.Warn("load open alerts", "error", err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, h := range open {
		e.state[key(h.RuleID, h.AgentID)] = &ruleState{
			status: h.Status, since: h.StartedAt, notified: h.Notified,
		}
	}
}

func (e *Engine) Tick(ctx context.Context) {
	rules, err := e.alerts.ListRules(ctx)
	if err != nil {
		e.logger.Warn("list alert rules", "error", err)
		return
	}
	channelList, err := e.notifyRepo.List(ctx)
	if err != nil {
		e.logger.Warn("list notify channels", "error", err)
		return
	}
	channels := make(map[int64]model.NotifyChannel, len(channelList))
	for _, c := range channelList {
		channels[c.ID] = c
	}
	agents, err := e.agents.List(ctx)
	if err != nil {
		e.logger.Warn("list agents for alerts", "error", err)
		return
	}
	now := e.now()
	e.mu.Lock()
	e.rules = map[int64]model.AlertRule{}
	for _, r := range rules {
		e.rules[r.ID] = r
	}
	e.channels = channels
	e.mu.Unlock()

	for _, rule := range rules {
		if !rule.Enabled {
			// Disabling a rule (or narrowing its scope) must close every open
			// alert it previously produced, not just the agents still in scope.
			for _, agent := range agents {
				e.resolve(ctx, rule, agent, now)
			}
			continue
		}
		for _, agent := range agents {
			if !inScope(rule.Scope, agent) {
				e.resolve(ctx, rule, agent, now)
				continue
			}
			online := e.hub.Online(agent.ID)
			if !online && rule.Metric != "offline" {
				// A stale last report must not keep a CPU/memory/disk alert
				// firing after the agent went offline.
				e.resolve(ctx, rule, agent, now)
				continue
			}
			report, hasReport := e.hub.Latest(agent.ID)
			value, ok := metricValue(rule.Metric, report, hasReport, online)
			if !ok {
				e.resolve(ctx, rule, agent, now)
				continue
			}
			e.evaluate(ctx, rule, agent, value, now)
		}
	}
}

func (e *Engine) evaluate(ctx context.Context, rule model.AlertRule, agent model.Agent, value float64, now time.Time) {
	k := key(rule.ID, agent.ID)
	condition := evaluateCondition(rule, value, agent, e.hub.Online(agent.ID))
	e.mu.Lock()
	st := e.state[k]
	if condition {
		if st == nil {
			st = &ruleState{status: model.AlertPending, since: now}
			e.state[k] = st
			e.mu.Unlock()
			e.persist(ctx, rule, agent, model.AlertPending, value, now, false)
			return
		}
		if st.status == model.AlertPending && now.Sub(st.since) >= time.Duration(rule.DurationSec)*time.Second {
			st.status = model.AlertFiring
			st.notified = false
			e.mu.Unlock()
			e.persist(ctx, rule, agent, model.AlertFiring, value, now, false)
			e.notify(ctx, rule, agent, model.AlertFiring, value, now)
			return
		}
		if st.status == model.AlertFiring {
			// notify applies its own 5-minute backoff and no-ops once delivered,
			// so a failed delivery is retried while the condition stays true.
			e.mu.Unlock()
			e.notify(ctx, rule, agent, model.AlertFiring, value, now)
			return
		}
		e.mu.Unlock()
		return
	}
	if st == nil {
		e.mu.Unlock()
		return
	}
	wasFiring := st.status == model.AlertFiring
	delete(e.state, k)
	e.mu.Unlock()
	e.persist(ctx, rule, agent, model.AlertResolved, value, now, true)
	if wasFiring {
		e.notify(ctx, rule, agent, model.AlertResolved, value, now)
	}
}

func (e *Engine) resolve(ctx context.Context, rule model.AlertRule, agent model.Agent, at time.Time) {
	k := key(rule.ID, agent.ID)
	e.mu.Lock()
	st := e.state[k]
	if st == nil {
		e.mu.Unlock()
		return
	}
	wasFiring := st.status == model.AlertFiring
	delete(e.state, k)
	e.mu.Unlock()
	e.persist(ctx, rule, agent, model.AlertResolved, 0, at, true)
	if wasFiring {
		e.notify(ctx, rule, agent, model.AlertResolved, 0, at)
	}
}

func (e *Engine) persist(ctx context.Context, rule model.AlertRule, agent model.Agent, status string, value float64, at time.Time, notified bool) {
	if err := e.alerts.UpsertHistory(ctx, rule.ID, agent.ID, status, value, at, notified); err != nil {
		e.logger.Warn("persist alert history", "error", err)
	}
}

func (e *Engine) notify(ctx context.Context, rule model.AlertRule, agent model.Agent, status string, value float64, now time.Time) {
	k := key(rule.ID, agent.ID)
	e.mu.Lock()
	st := e.state[k]
	if status == model.AlertFiring {
		if st == nil || st.notified || now.Sub(st.lastAttempt) < 5*time.Minute {
			e.mu.Unlock()
			return
		}
		st.lastAttempt = now
	}
	channels := make([]model.NotifyChannel, 0, len(rule.NotifyChannelIDs))
	for _, id := range rule.NotifyChannelIDs {
		if c, ok := e.channels[id]; ok {
			channels = append(channels, c)
		}
	}
	e.mu.Unlock()

	title := fmt.Sprintf("[%s] %s", status, rule.Name)
	message := fmt.Sprintf("%s\n节点: %s\n指标: %s\n当前值: %.2f\n时间: %s",
		title, displayName(agent), rule.Metric, value, now.Format(time.RFC3339))
	sent := 0
	for _, channel := range channels {
		if err := e.notifier.Send(ctx, channel, title, message); err != nil {
			e.logger.Warn("send alert", "channel", channel.Name, "error", err)
			continue
		}
		sent++
	}
	if status == model.AlertFiring && (sent > 0 || len(channels) == 0) {
		e.mu.Lock()
		if current := e.state[k]; current != nil {
			current.notified = true
		}
		e.mu.Unlock()
		e.persist(ctx, rule, agent, model.AlertFiring, value, now, true)
	}
}

func evaluateCondition(rule model.AlertRule, value float64, agent model.Agent, online bool) bool {
	if rule.Metric == "offline" {
		return !online
	}
	switch rule.Operator {
	case ">":
		return value > rule.Threshold
	case ">=":
		return value >= rule.Threshold
	case "<":
		return value < rule.Threshold
	case "<=":
		return value <= rule.Threshold
	case "=", "==":
		return value == rule.Threshold
	default:
		return false
	}
}

func metricValue(metric string, report wire.Report, hasReport bool, online bool) (float64, bool) {
	switch metric {
	case "offline":
		if online {
			return 0, true
		}
		return 1, true
	case "cpu":
		if !hasReport {
			return 0, false
		}
		return report.Data.CPU.Usage, true
	case "memory":
		if !hasReport || report.Data.Memory.Total == 0 {
			return 0, false
		}
		return float64(report.Data.Memory.Used) * 100 / float64(report.Data.Memory.Total), true
	case "disk":
		if !hasReport {
			return 0, false
		}
		max := 0.0
		for _, d := range report.Data.Disk {
			if d.Total > 0 {
				if p := float64(d.Used) * 100 / float64(d.Total); p > max {
					max = p
				}
			}
		}
		return max, true
	default:
		return 0, false
	}
}

func inScope(scope model.AlertScope, agent model.Agent) bool {
	switch scope.Type {
	case "", "all":
		return true
	case "agent_ids":
		for _, id := range scope.IDs {
			if id == agent.ID {
				return true
			}
		}
		return false
	case "tag_ids":
		var tags []int64
		if err := json.Unmarshal([]byte(agent.TagIDs), &tags); err != nil {
			return false
		}
		for _, want := range scope.IDs {
			for _, have := range tags {
				if want == have {
					return true
				}
			}
		}
	}
	return false
}

func key(ruleID, agentID int64) string {
	return fmt.Sprintf("%d/%d", ruleID, agentID)
}

// DisplayNameOrHostname is a small helper kept here to avoid importing the UI.
func displayName(agent model.Agent) string {
	if strings.TrimSpace(agent.DisplayName) != "" {
		return agent.DisplayName
	}
	return agent.Hostname
}
