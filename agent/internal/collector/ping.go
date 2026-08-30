package collector

import (
	"context"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/YCJE/XProbe/internal/model"
)

// Ping 参数(设计文档 4.6, 超越 Nezha 的采样设计)。
const (
	ICMPCount    = 10          // 10 包采样
	ICMPInterval = 500 * time.Millisecond
	ICMPTimeout  = 15 * time.Second
	TCPCount     = 5           // 5 次采样
	TCPTimeout   = 5 * time.Second
)

// PingMethod 采集方式: auto=privileged ICMP → unprivileged ICMP → TCP 降级链。
type PingMethod string

const (
	PingAuto PingMethod = "auto"
	PingICMP PingMethod = "icmp"
	PingTCP  PingMethod = "tcp"
)

// PingCollector 三网探测(设计文档 4.5/4.6)。
// DNS 预解析: 探测前解析为 IP, 排除 DNS 时间只测网络延迟。
type PingCollector struct {
	Targets func() []model.PingTarget // 目标来源(配置拉取缓存)
	Method  PingMethod                // 配置: auto/icmp/tcp

	mu     sync.Mutex
	chosen string // 实际采用的探测方式(icmp/icmp_unprivileged/tcp)
}

func NewPingCollector(method PingMethod) *PingCollector {
	return &PingCollector{Method: method, chosen: ""}
}

// ChosenMethod 返回实际采用的探测方式(面板标注)。
func (p *PingCollector) ChosenMethod() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chosen
}

// Collect 执行一轮完整探测(默认 60s 间隔由上报器调度)。
func (p *PingCollector) Collect(ctx context.Context) ([]model.PingResult, error) {
	if p.Targets == nil {
		return nil, nil
	}
	targets := p.Targets()
	if len(targets) == 0 {
		return nil, nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	out := make([]model.PingResult, 0, len(targets))
	for _, t := range targets {
		wg.Add(1)
		go func(t model.PingTarget) {
			defer wg.Done()
			r, err := p.pingOne(ctx, t)
			if err != nil {
				return // 单目标失败(解析失败/无 v6)不影响其余
			}
			mu.Lock()
			out = append(out, r)
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (p *PingCollector) pingOne(ctx context.Context, t model.PingTarget) (model.PingResult, error) {
	// 预解析排除 DNS 时间; v6 目标在无 IPv6 出口时静默跳过
	ip, err := resolve(t.Target, t.IPVersion)
	if err != nil {
		return model.PingResult{}, err
	}

	port := ""
	if i := lastIndexOfColon(t.Target); i >= 0 && net.ParseIP(t.Target) == nil {
		port = t.Target[i:] // 带端口 → TCP Ping(设计文档 4.5.1)
	}
	forceTCP := p.Method == PingTCP || port != ""

	var rtts []float64
	sent, recv := 0, 0
	method := ""

	switch {
	case forceTCP:
		rtts, sent, recv, err = p.tcp(ctx, ip, port)
		method = "tcp"
	case p.Method == PingICMP:
		// 配置强制 ICMP(仍按 privileged → unprivileged 降级), 失败则跳过该目标
		rtts, sent, recv, err = p.icmp(ctx, ip)
		method = "icmp"
	default: // auto: ICMP → TCP
		rtts, sent, recv, err = p.icmp(ctx, ip)
		if err != nil {
			rtts, sent, recv, err = p.tcp(ctx, ip, port)
			method = "tcp"
		} else {
			method = "icmp"
		}
	}
	if err != nil {
		return model.PingResult{}, err
	}

	p.mu.Lock()
	if method != "" {
		p.chosen = method
	}
	p.mu.Unlock()

	return computeStats(t, rtts, sent, recv, method), nil
}

// icmp 尝试 privileged(S4: setcap CAP_NET_RAW)→ unprivileged(ping_group_range)降级。
func (p *PingCollector) icmp(ctx context.Context, ip string) ([]float64, int, int, error) {
	for _, privileged := range []bool{true, false} {
		rtts, sent, recv, err := icmpOnce(ctx, ip, privileged)
		if err == nil {
			return rtts, sent, recv, nil
		}
		if ctx.Err() != nil {
			return nil, 0, 0, ctx.Err()
		}
	}
	return nil, 0, 0, fmt.Errorf("icmp unavailable both modes")
}

func (p *PingCollector) icmpLabel() string {
	return "icmp" // privileged/unprivileged 细分由启动探测标注; 上报统一 icmp
}

func icmpOnce(ctx context.Context, ip string, privileged bool) ([]float64, int, int, error) {
	pinger := probing.New(ip)
	pinger.Count = ICMPCount
	pinger.Interval = ICMPInterval
	pinger.Timeout = ICMPTimeout
	pinger.SetPrivileged(privileged)
	if err := pinger.Run(); err != nil {
		return nil, 0, 0, err
	}
	st := pinger.Statistics()
	rtts := make([]float64, 0, len(st.Rtts))
	for _, r := range st.Rtts {
		rtts = append(rtts, float64(r.Microseconds())/1000.0)
	}
	return rtts, st.PacketsSent, st.PacketsRecv, nil
}

// tcpPing TCP 握手延迟(降级方案, 设计文档 4.6)。
func (p *PingCollector) tcp(ctx context.Context, ip, port string) ([]float64, int, int, error) {
	if port == "" {
		port = ":80" // 无端口目标降级 TCP 时探测 80
	}
	addr := net.JoinHostPort(ip, strings.TrimPrefix(port, ":"))
	var rtts []float64
	sent, recv := 0, 0
	dialer := &net.Dialer{Timeout: TCPTimeout}
	for i := 0; i < TCPCount; i++ {
		sent++
		if ctx.Err() != nil {
			return nil, sent, recv, ctx.Err()
		}
		start := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		rtt := time.Since(start)
		if err == nil {
			conn.Close()
			recv++
			rtts = append(rtts, float64(rtt.Microseconds())/1000.0)
		}
		select {
		case <-ctx.Done():
			return rtts, sent, recv, ctx.Err()
		case <-time.After(ICMPInterval):
		}
	}
	if recv == 0 {
		return rtts, sent, recv, fmt.Errorf("tcp ping: all attempts failed to %s", addr)
	}
	return rtts, sent, recv, nil
}

func strings_TrimPrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }

func lastIndexOfColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

// resolve DNS 预解析(设计文档 4.6: 排除 DNS 时间); v6 目标无 v6 出口时报错跳过。
func resolve(host string, ipVersion int) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ipVersion == 6 && ip.To4() != nil {
			return "", fmt.Errorf("target %s is v4 but marked v6", host)
		}
		return ip.String(), nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", host, err)
	}
	want6 := ipVersion == 6
	for _, ip := range ips {
		if (ip.To4() == nil) == want6 {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("resolve %s: no matching A/AAAA record", host)
}

// computeStats 统一统计口径(设计文档 4.5): avg/min/max/jitter(StdDevRtt)/loss。
func computeStats(t model.PingTarget, rtts []float64, sent, recv int, method string) model.PingResult {
	r := model.PingResult{
		Target: t.Target, Name: t.Name, Method: method,
		IPVersion: t.IPVersion, PacketsSent: sent, PacketsRecv: recv,
	}
	if sent > 0 {
		r.Loss = float64(sent-recv) / float64(sent) * 100
	}
	if len(rtts) == 0 {
		r.AvgLatency = 60000 // 全丢包: 以超时上限上报
		r.Loss = 100
		return r
	}
	sort.Float64s(rtts)
	var sum float64
	for _, v := range rtts {
		sum += v
	}
	r.AvgLatency = sum / float64(len(rtts))
	r.MinLatency = rtts[0]
	r.MaxLatency = rtts[len(rtts)-1]
	var variance float64
	for _, v := range rtts {
		variance += (v - r.AvgLatency) * (v - r.AvgLatency)
	}
	r.Jitter = math.Sqrt(variance / float64(len(rtts)))
	return r
}
