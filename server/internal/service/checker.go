// Package service — 服务监控拨测器(Nezha 对标, ROADMAP P2):
// Server 主动对 HTTP/TCP/ICMP 端点探活, 产出在线率与故障时间线, 不涉及 Agent 通道(S1 不受影响)。
package service

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

const (
	svcMaxRecent       = 64 // 状态页最近 N 次条
	svcDailyDays       = 45
	svcProbeTimeout    = 10 * time.Second
	svcDefaultInterval = 60 * time.Second
)

// ServiceNotifier 服务告警发送抽象(复用 Notifier)。
type ServiceNotifier interface {
	Send(ctx context.Context, channelID int64, title, body string) error
}

// ServiceChecker 服务拨测引擎: 按各服务 interval 周期探活,
// 状态转移(down/up)时通知, 结果滚动保留 + 日汇总落库。
type ServiceChecker struct {
	repo     *repository.ServiceRepo
	notifier ServiceNotifier

	mu      sync.Mutex
	recent  map[int64][]model.ServiceResult
	states  map[int64]bool // 当前在线状态
	since   map[int64]time.Time
	lastRun map[int64]time.Time
	dayAcc  map[int64]*svcDayAcc
	now     func() time.Time
}

type svcDayAcc struct {
	date      string
	total, ok int64
}

func NewServiceChecker(repo *repository.ServiceRepo, notifier ServiceNotifier) *ServiceChecker {
	return &ServiceChecker{
		repo: repo, notifier: notifier,
		recent: map[int64][]model.ServiceResult{}, states: map[int64]bool{},
		since: map[int64]time.Time{}, lastRun: map[int64]time.Time{},
		dayAcc: map[int64]*svcDayAcc{}, now: time.Now,
	}
}

// Run 主循环: 全局 5s 粒度 tick, 到期的服务各自探测。
func (c *ServiceChecker) Run(ctx context.Context) {
	c.SeedDaily(ctx)
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

func (c *ServiceChecker) tick(ctx context.Context) {
	services, err := c.repo.ListEnabled(ctx)
	if err != nil {
		log.Printf("[service] list: %v", err)
		return
	}
	now := c.now()
	for i := range services {
		svc := &services[i]
		interval := svcDefaultInterval
		if svc.IntervalSec > 0 {
			interval = time.Duration(svc.IntervalSec) * time.Second
		}
		c.mu.Lock()
		last := c.lastRun[svc.ID]
		due := now.Sub(last) >= interval
		if due {
			c.lastRun[svc.ID] = now
		}
		c.mu.Unlock()
		if due {
			go c.probe(ctx, svc) // 单服务超时不拖累其他服务
		}
	}
	// 日汇总落盘(每 tick 检查一次, 内部有状态去重)
	c.flushDaily(ctx, now)
}

// probe 执行一次探活: 更新最近结果与在线状态, 状态转移时通知。
func (c *ServiceChecker) probe(ctx context.Context, svc *model.Service) {
	start := c.now()
	ok, perr := c.probeOnce(ctx, svc)
	latency := 0.0
	if ok {
		latency = float64(c.now().Sub(start).Microseconds()) / 1000.0
	}
	res := model.ServiceResult{Ts: start.Unix(), OK: ok, LatencyMs: latency}

	// 日累计(内存为准, flushDaily 覆盖当日行)
	today := c.now().UTC().Format("2006-01-02")
	c.mu.Lock()
	acc := c.dayAcc[svc.ID]
	if acc == nil || acc.date != today {
		acc = &svcDayAcc{date: today}
		c.dayAcc[svc.ID] = acc
	}
	acc.total++
	if res.OK {
		acc.ok++
	}
	prev, had := c.states[svc.ID]
	c.states[svc.ID] = res.OK
	c.since[svc.ID] = start
	recent := append(c.recent[svc.ID], res)
	if len(recent) > svcMaxRecent {
		recent = recent[len(recent)-svcMaxRecent:]
	}
	c.recent[svc.ID] = recent
	channel := svc.NotifyChannelID
	name := svc.Name
	c.mu.Unlock()

	// 状态转移通知: 首次探测不触发, 仅 down→up / up→down
	if had && prev != res.OK && channel != nil && *channel > 0 {
		status, detail := "UP", "服务恢复在线"
		if !res.OK {
			status, detail = "DOWN", "服务不可达"
			if perr != nil {
				detail += ": " + perr.Error()
			}
		}
		cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		go func() {
			defer cancel()
			title := fmt.Sprintf("[XProbe] 服务%s: %s", status, name)
			if err := c.notifier.Send(cctx, *channel, title, detail); err != nil {
				log.Printf("[service] notify: %v", err)
			}
		}()
	}
}

// ProbeOncePublic 供 API 手动探活一次(不写状态、不入环形)。
func (c *ServiceChecker) ProbeOncePublic(ctx context.Context, svc *model.Service) (bool, error) {
	return c.probeOnce(ctx, svc)
}

// probeOnce 单次探活: http/tcp/icmp。
func (c *ServiceChecker) probeOnce(ctx context.Context, svc *model.Service) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, svcProbeTimeout)
	defer cancel()
	switch svc.Type {
	case "http":
		return c.probeHTTP(ctx, svc)
	case "tcp":
		return c.probeTCP(ctx, svc)
	case "icmp":
		return c.probeICMP(ctx, svc)
	default:
		return false, fmt.Errorf("unknown service type %q", svc.Type)
	}
}

// probeHTTP GET 探测: 2xx/3xx 视为成功。目标为管理员配置(允许内网, 用于局域网服务监控)。
func (c *ServiceChecker) probeHTTP(ctx context.Context, svc *model.Service) (bool, error) {
	url := svc.Target
	if svc.Port > 0 {
		url = fmt.Sprintf("%s:%d%s", strings_TrimRightSlash(url), svc.Port, svc.Path)
	} else if svc.Path != "" {
		url = strings_TrimRightSlash(url) + svc.Path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := (&http.Client{Timeout: svcProbeTimeout}).Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400, nil
}

func strings_TrimRightSlash(s string) string {
	return strings.TrimRight(s, "/")
}

// probeTCP 握手探测。
func (c *ServiceChecker) probeTCP(ctx context.Context, svc *model.Service) (bool, error) {
	port := svc.Port
	if port == 0 {
		port = 80
	}
	d := &net.Dialer{Timeout: svcProbeTimeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(svc.Target, strconv.Itoa(port)))
	if err != nil {
		return false, err
	}
	conn.Close()
	return true, nil
}

// probeICMP 3 包探测(privileged 失败自动降级 unprivileged, 与 Agent 同策略)。
func (c *ServiceChecker) probeICMP(ctx context.Context, svc *model.Service) (bool, error) {
	for _, privileged := range []bool{true, false} {
		pinger := probing.New(svc.Target)
		pinger.Count = 3
		pinger.Interval = 300 * time.Millisecond
		pinger.Timeout = svcProbeTimeout
		pinger.SetPrivileged(privileged)
		if err := pinger.Run(); err == nil {
			return pinger.Statistics().PacketsRecv > 0, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	return false, fmt.Errorf("icmp unavailable both modes")
}

// SeedDaily 重启后从库里回填当日累计(使覆盖式落库跨重启一致)。
func (c *ServiceChecker) SeedDaily(ctx context.Context) {
	today := c.now().UTC().Format("2006-01-02")
	services, err := c.repo.ListEnabled(ctx)
	if err != nil {
		return
	}
	for _, svc := range services {
		daily, derr := c.repo.ListDaily(ctx, svc.ID, 1)
		if derr != nil || len(daily) == 0 || daily[0].Date != today {
			continue
		}
		c.mu.Lock()
		c.dayAcc[svc.ID] = &svcDayAcc{date: today, total: daily[0].Total, ok: daily[0].Ok}
		c.mu.Unlock()
	}
}

// Snapshot 输出状态页/面板载荷(白名单字段)。
func (c *ServiceChecker) Snapshot(ctx context.Context) []model.ServiceStatus {
	services, err := c.repo.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]model.ServiceStatus, 0, len(services))
	for _, svc := range services {
		if !svc.Enabled {
			continue
		}
		st := model.ServiceStatus{
			ID: svc.ID, Name: svc.Name, Type: svc.Type,
			Recent: []model.ServiceResult{}, Daily: []model.ServiceDaily{},
		}
		c.mu.Lock()
		if rs := c.recent[svc.ID]; len(rs) > 0 {
			last := rs[len(rs)-1]
			st.Up = last.OK
			st.Latency = last.LatencyMs
			cp := make([]model.ServiceResult, len(rs))
			copy(cp, rs)
			st.Recent = cp
		} else {
			st.Up = c.states[svc.ID]
		}
		c.mu.Unlock()
		if daily, derr := c.repo.ListDaily(ctx, svc.ID, svcDailyDays); derr == nil {
			st.Daily = daily
			var tot float64
			for _, d := range daily {
				tot += d.UpRatio
			}
			if len(daily) > 0 {
				st.Uptime45 = tot / float64(len(daily))
			}
		}
		out = append(out, st)
	}
	return out
}

// flushDaily 把当日累计写入 service_daily(幂等: 以累计值覆盖当日行)。
func (c *ServiceChecker) flushDaily(ctx context.Context, now time.Time) {
	c.mu.Lock()
	type kv struct {
		id  int64
		acc *svcDayAcc
	}
	var batch []kv
	for id, acc := range c.dayAcc {
		if acc.total > 0 {
			cp := *acc
			batch = append(batch, kv{id, &cp})
		}
	}
	c.mu.Unlock()
	for _, b := range batch {
		ratio := 0.0
		if b.acc.total > 0 {
			ratio = float64(b.acc.ok) / float64(b.acc.total) * 100
		}
		d := model.ServiceDaily{Date: b.acc.date, Total: b.acc.total, Ok: b.acc.ok, UpRatio: ratio}
		if err := c.repo.UpsertDaily(ctx, b.id, d); err != nil {
			log.Printf("[service] daily upsert: %v", err)
		}
	}
	_ = now
}
