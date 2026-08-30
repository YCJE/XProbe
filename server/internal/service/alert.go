package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// 告警状态(设计文档 5.4)。
const (
	StatusPending  = "PENDING"
	StatusFiring   = "FIRING"
	StatusResolved = "RESOLVED"
)

type alertState struct {
	status       string // PENDING / FIRING
	startedAt    time.Time
	lastNotified time.Time
}

// Sender 通知发送抽象(测试注入)。
type Sender interface {
	Send(ctx context.Context, channelID int64, title, body string) error
}

// AlertEngine 告警引擎: OK→PENDING→FIRING→RESOLVED 状态机,
// 状态持久化到 alert_history(Server 重启恢复现场, 不重复通知, 设计文档 5.4/10.7)。
type AlertEngine struct {
	rules    *repository.AlertRepo
	agents   *repository.AgentRepo
	hub      *Hub
	notifier Sender

	silence time.Duration // FIRING 中重复通知的最小间隔, 默认 60 分钟
	now     func() time.Time

	mu    sync.Mutex
	state map[string]*alertState // key: ruleID:agentID
}

func NewAlertEngine(rules *repository.AlertRepo, agents *repository.AgentRepo,
	hub *Hub, notifier Sender) *AlertEngine {
	e := &AlertEngine{
		rules: rules, agents: agents, hub: hub, notifier: notifier,
		silence: 60 * time.Minute, now: time.Now,
		state: map[string]*alertState{},
	}
	// 离线事件接线(即时 FIRING/RESOLVED, 设计文档 5.4 agent_offline)
	hub.SetOnOffline(func(agentID int64) { e.HandleOffline(context.Background(), agentID) })
	hub.SetOnOnline(func(agentID int64) { e.HandleOnline(context.Background(), agentID) })
	return e
}

// Restore Server 重启后加载未恢复状态。
func (e *AlertEngine) Restore(ctx context.Context) error {
	open, err := e.rules.LoadOpen(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, h := range open {
		st := &alertState{startedAt: time.Unix(h.StartedAt, 0)}
		if h.Status == StatusFiring {
			st.status = StatusFiring
			st.lastNotified = time.Unix(h.UpdatedAt, 0)
		} else {
			st.status = StatusPending
		}
		e.state[stateKey(h.RuleID, h.AgentID)] = st
	}
	if len(open) > 0 {
		log.Printf("[alert] restored %d open alert states", len(open))
	}
	return nil
}

// Run 主循环(默认 30s 评估一次)。
func (e *AlertEngine) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := e.Evaluate(ctx); err != nil {
				log.Printf("[alert] evaluate: %v", err)
			}
		}
	}
}

// Evaluate 全量评估所有启用规则 × 全部 Agent。
func (e *AlertEngine) Evaluate(ctx context.Context) error {
	rules, err := e.rules.ListRules(ctx)
	if err != nil {
		return err
	}
	agents, err := e.agents.List(ctx)
	if err != nil {
		return err
	}
	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled {
			continue
		}
		if rule.Metric == model.MetricOffline {
			continue // 离线由事件驱动(Hook), 非轮询
		}
		for j := range agents {
			agent := &agents[j]
			met, value := e.check(rule, agent)
			e.transition(ctx, rule, agent.ID, met, value)
		}
	}
	return nil
}

// check 评估单指标, 返回 (是否超阈值, 当前值)。
func (e *AlertEngine) check(rule *model.AlertRule, agent *model.Agent) (bool, *float64) {
	switch rule.Metric {
	case model.MetricCPU:
		r, ok := e.hub.LatestReport(agent.ID)
		if !ok || r.CPU.Usage == nil {
			return false, nil
		}
		return compare(*r.CPU.Usage, rule.Operator, rule.Threshold), r.CPU.Usage
	case model.MetricMem:
		r, ok := e.hub.LatestReport(agent.ID)
		if !ok || r.Memory.Total == 0 {
			return false, nil
		}
		v := float64(r.Memory.Used) / float64(r.Memory.Total) * 100
		return compare(v, rule.Operator, rule.Threshold), &v
	case model.MetricDisk:
		r, ok := e.hub.LatestReport(agent.ID)
		if !ok || len(r.Disk) == 0 {
			return false, nil
		}
		max := 0.0
		for _, d := range r.Disk {
			if d.Total > 0 {
				if p := float64(d.Used) / float64(d.Total) * 100; p > max {
					max = p
				}
			}
		}
		return compare(max, rule.Operator, rule.Threshold), &max
	case model.MetricTrafficQuot:
		if agent.TrafficQuotaBytes <= 0 {
			return false, nil // 无配额不告警
		}
		r, ok := e.hub.LatestReport(agent.ID)
		if !ok {
			return false, nil
		}
		used := float64(r.TrafficMonthly.RxBytes + r.TrafficMonthly.TxBytes)
		v := used / float64(agent.TrafficQuotaBytes) * 100
		return compare(v, rule.Operator, rule.Threshold), &v
	case model.MetricExpireDays:
		if agent.ExpiresAt <= 0 {
			return false, nil
		}
		days := float64(time.Until(time.Unix(agent.ExpiresAt, 0)) / (24 * time.Hour))
		return compare(days, rule.Operator, rule.Threshold), &days
	default:
		return false, nil
	}
}

func compare(v float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return v > threshold
	case "<":
		return v < threshold
	case "=":
		return v == threshold
	default:
		return false
	}
}

// transition 单 (rule, agent) 状态机推进。
func (e *AlertEngine) transition(ctx context.Context, rule *model.AlertRule, agentID int64, met bool, value *float64) {
	key := stateKey(rule.ID, agentID)
	now := e.now()

	e.mu.Lock()
	st := e.state[key]
	if !met {
		if st != nil {
			delete(e.state, key)
			e.mu.Unlock()
			e.notify(ctx, rule, agentID, StatusResolved, value, "已恢复")
			_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusResolved, value, true, now.Unix())
		}
		return
	}
	if st == nil {
		e.state[key] = &alertState{status: StatusPending, startedAt: now}
		e.mu.Unlock()
		_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusPending, value, false, now.Unix())
		return
	}
	switch st.status {
	case StatusPending:
		if now.Sub(st.startedAt) >= time.Duration(rule.Duration)*time.Second {
			st.status = StatusFiring
			st.lastNotified = now
			e.mu.Unlock()
			log.Printf("[alert][security] rule=%d agent=%d FIRING value=%v", rule.ID, agentID, value)
			_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusFiring, value, true, now.Unix())
			e.notify(ctx, rule, agentID, StatusFiring, value, fmt.Sprintf("当前值 %v", value))
			return
		}
		_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusPending, value, false, now.Unix())
	case StatusFiring:
		// 静默期(设计文档 5.4): FIRING 期间超过静默期仍未恢复则再次通知
		if now.Sub(st.lastNotified) >= e.silence {
			st.lastNotified = now
			e.mu.Unlock()
			_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusFiring, value, true, now.Unix())
			e.notify(ctx, rule, agentID, StatusFiring, value, fmt.Sprintf("仍未恢复, 当前值 %v", value))
			return
		}
		_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusFiring, value, true, now.Unix())
	}
	e.mu.Unlock()
}

// HandleOffline 离线事件: agent_offline 规则即时 FIRING(设计文档 5.4)。
func (e *AlertEngine) HandleOffline(ctx context.Context, agentID int64) {
	rules, err := e.rules.ListRules(ctx)
	if err != nil {
		return
	}
	now := e.now()
	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled || rule.Metric != model.MetricOffline {
			continue
		}
		e.mu.Lock()
		e.state[stateKey(rule.ID, agentID)] = &alertState{status: StatusFiring, startedAt: now, lastNotified: now}
		e.mu.Unlock()
		_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusFiring, nil, true, now.Unix())
		e.notify(ctx, rule, agentID, StatusFiring, nil, "服务器离线")
	}
}

// HandleOnline 上线事件: 离线告警恢复。
func (e *AlertEngine) HandleOnline(ctx context.Context, agentID int64) {
	rules, err := e.rules.ListRules(ctx)
	if err != nil {
		return
	}
	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled || rule.Metric != model.MetricOffline {
			continue
		}
		key := stateKey(rule.ID, agentID)
		e.mu.Lock()
		_, was := e.state[key]
		delete(e.state, key)
		e.mu.Unlock()
		if was {
			_ = e.rules.UpsertState(ctx, rule.ID, agentID, StatusResolved, nil, true, e.now().Unix())
			e.notify(ctx, rule, agentID, StatusResolved, nil, "服务器已恢复上线")
		}
	}
}

// notify 异步发送通知(失败仅记录, 不阻塞评估循环)。
func (e *AlertEngine) notify(ctx context.Context, rule *model.AlertRule, agentID int64, status string, value *float64, detail string) {
	if rule.NotifyChannelID == nil || *rule.NotifyChannelID <= 0 {
		return
	}
	title := fmt.Sprintf("[XProbe] %s: 规则 %q Agent #%d", status, rule.Name, agentID)
	if value != nil {
		title += fmt.Sprintf(" (值 %.1f)", *value)
	}
	body := detail + "\n时间: " + e.now().Format(time.RFC3339)
	cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	go func() {
		defer cancel()
		if err := e.notifier.Send(cctx, *rule.NotifyChannelID, title, body); err != nil {
			log.Printf("[alert] notify failed: %v", err)
		}
	}()
}

func stateKey(ruleID, agentID int64) string {
	return fmt.Sprintf("%d:%d", ruleID, agentID)
}
