// Package ping executes the read-only probes configured by the hub. It
// supports ICMP (unprivileged when available), TCP connect and HTTP HEAD.
package ping

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"
	"probe/common/wire"
)

const (
	icmpCount    = 5
	tcpCount     = 5
	httpCount    = 3
	probeTimeout = 5 * time.Second
)

// Run probes one target. preference is auto/icmp/tcp from the agent config.
func Run(ctx context.Context, target wire.PingTarget, preference string) wire.PingStat {
	protocol := strings.ToLower(strings.TrimSpace(target.Protocol))
	if preference == "tcp" {
		return tcpPing(ctx, target)
	}
	switch protocol {
	case "tcp":
		return tcpPing(ctx, target)
	case "http":
		return httpPing(ctx, target)
	}
	if preference == "icmp" {
		return icmpPing(ctx, target)
	}
	stat, err := icmpPingErr(ctx, target)
	if err == nil {
		return stat
	}
	// No CAP_NET_RAW / ping_group_range: degrade honestly to TCP.
	return tcpPing(ctx, target)
}

func icmpPing(ctx context.Context, target wire.PingTarget) wire.PingStat {
	stat, _ := icmpPingErr(ctx, target)
	return stat
}

func icmpPingErr(ctx context.Context, target wire.PingTarget) (wire.PingStat, error) {
	host := icmpHost(target.Target)
	if host == "" {
		return failure(target, "icmp", icmpCount), fmt.Errorf("empty host")
	}
	pinger, err := probing.NewPinger(host)
	if err != nil {
		return failure(target, "icmp", icmpCount), err
	}
	pinger.SetPrivileged(false)
	pinger.Count = icmpCount
	pinger.Interval = 300 * time.Millisecond
	pinger.Timeout = probeTimeout
	if target.IPVersion == 6 {
		pinger.SetNetwork("ip6")
	} else {
		pinger.SetNetwork("ip4")
	}
	done := make(chan error, 1)
	go func() { done <- pinger.Run() }()
	select {
	case <-ctx.Done():
		pinger.Stop()
		return failure(target, "icmp", icmpCount), ctx.Err()
	case err := <-done:
		if err != nil {
			return failure(target, "icmp", icmpCount), err
		}
	}
	stats := pinger.Statistics()
	if stats.PacketsSent == 0 {
		return failure(target, "icmp", icmpCount), fmt.Errorf("no packets sent")
	}
	stat := wire.PingStat{
		Target: target.Target, Name: target.Name, Method: "icmp",
		IPVersion:  target.IPVersion,
		AvgLatency: ms(stats.AvgRtt), MinLatency: ms(stats.MinRtt), MaxLatency: ms(stats.MaxRtt),
		Jitter: ms(stats.StdDevRtt), Loss: stats.PacketLoss,
		PacketsSent: stats.PacketsSent, PacketsRecv: stats.PacketsRecv,
	}
	return stat, nil
}

func tcpPing(ctx context.Context, target wire.PingTarget) wire.PingStat {
	host, port := splitTarget(target.Target, "443")
	if host == "" {
		return failure(target, "tcp", tcpCount)
	}
	address := net.JoinHostPort(host, port)
	network := dialNetwork(target.IPVersion)
	var latencies []float64
	sent := 0
	for i := 0; i < tcpCount; i++ {
		sent++
		start := time.Now()
		dialCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, network, address)
		cancel()
		if err == nil {
			latencies = append(latencies, float64(time.Since(start).Nanoseconds())/1e6)
			_ = conn.Close()
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return summarize(target, "tcp", latencies, sent)
}

func httpPing(ctx context.Context, target wire.PingTarget) wire.PingStat {
	raw := target.Target
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return failure(target, "http", httpCount)
	}
	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(dialCtx, dialNetwork(target.IPVersion), address)
		},
		TLSHandshakeTimeout: probeTimeout,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   probeTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	var latencies []float64
	sent := 0
	for i := 0; i < httpCount; i++ {
		sent++
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, parsed.String(), nil)
		if err != nil {
			break
		}
		req.Header.Set("User-Agent", "XProbe/0.2 (read-only probe)")
		start := time.Now()
		resp, err := client.Do(req)
		if err == nil {
			latencies = append(latencies, float64(time.Since(start).Nanoseconds())/1e6)
			_ = resp.Body.Close()
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return summarize(target, "http", latencies, sent)
}

func summarize(target wire.PingTarget, method string, latencies []float64, sent int) wire.PingStat {
	stat := failure(target, method, sent)
	stat.PacketsSent = sent
	stat.PacketsRecv = len(latencies)
	if len(latencies) == 0 {
		return stat
	}
	stat.Loss = float64(sent-len(latencies)) / float64(sent) * 100
	stat.AvgLatency = avg(latencies)
	stat.MinLatency = min(latencies)
	stat.MaxLatency = max(latencies)
	stat.Jitter = stddev(latencies, stat.AvgLatency)
	return stat
}

func failure(target wire.PingTarget, method string, sent int) wire.PingStat {
	return wire.PingStat{
		Target: target.Target, Name: target.Name, Method: method,
		IPVersion: target.IPVersion, Loss: 100, PacketsSent: sent,
	}
}

func icmpHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(raw, "[]")
}

func dialNetwork(ipVersion int) string {
	if ipVersion == 6 {
		return "tcp6"
	}
	return "tcp4"
}

func splitTarget(raw, defaultPort string) (string, string) {
	raw = strings.TrimSpace(raw)
	if host, port, err := net.SplitHostPort(raw); err == nil {
		return strings.Trim(host, "[]"), port
	}
	// A bare IPv6 address contains colons but no port; keep it whole.
	if ip := net.ParseIP(strings.Trim(raw, "[]")); ip != nil {
		return strings.Trim(raw, "[]"), defaultPort
	}
	return raw, defaultPort
}

func ms(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / 1e6
}

func avg(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func min(values []float64) float64 {
	out := values[0]
	for _, v := range values[1:] {
		if v < out {
			out = v
		}
	}
	return out
}

func max(values []float64) float64 {
	out := values[0]
	for _, v := range values[1:] {
		if v > out {
			out = v
		}
	}
	return out
}

func stddev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += (v - mean) * (v - mean)
	}
	return math.Sqrt(sum / float64(len(values)))
}
