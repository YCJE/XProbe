package collector

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

func TestComputeStats(t *testing.T) {
	target := model.PingTarget{Target: "114.114.114.114", Name: "电信", IPVersion: 4}
	// 10 发 8 收: 全量统计(设计文档 4.6)
	r := computeStats(target, []float64{10, 12, 14, 16, 12, 12, 13, 11}, 10, 8, "icmp")
	if r.PacketsSent != 10 || r.PacketsRecv != 8 {
		t.Fatalf("packets = %d/%d", r.PacketsSent, r.PacketsRecv)
	}
	if r.Loss != 20 {
		t.Fatalf("loss = %v, want 20", r.Loss)
	}
	// avg = (10+11+12*3+13+14+16)/8 = 12.5
	if r.AvgLatency < 12.49 || r.AvgLatency > 12.51 {
		t.Fatalf("avg = %v", r.AvgLatency)
	}
	if r.MinLatency != 10 || r.MaxLatency != 16 {
		t.Fatalf("min/max = %v/%v", r.MinLatency, r.MaxLatency)
	}
	if r.Jitter <= 0 {
		t.Fatalf("jitter = %v, want >0", r.Jitter)
	}
}

func TestComputeStats_AllLost(t *testing.T) {
	r := computeStats(model.PingTarget{Target: "x", IPVersion: 4}, nil, 10, 0, "icmp")
	if r.Loss != 100 || r.AvgLatency != 60000 {
		t.Fatalf("all-lost = %+v", r)
	}
}

func TestTCPPing_RealConnection(t *testing.T) {
	// 真实 TCP 连接(本机回环, 无需特权)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	host := srv.Listener.Addr().(*net.TCPAddr).IP.String()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	p := NewPingCollector(PingTCP)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rtts, sent, recv, err := p.tcp(ctx, host, fmtPort(port))
	if err != nil {
		t.Fatalf("tcp: %v", err)
	}
	if sent != TCPCount || recv == 0 || len(rtts) != recv {
		t.Fatalf("sent/recv/rtts = %d/%d/%d", sent, recv, len(rtts))
	}
	for _, r := range rtts {
		if r < 0 || r > 5000 {
			t.Fatalf("rtt out of range: %v", r)
		}
	}
}

func TestTCPPing_Unreachable(t *testing.T) {
	// 不可达端口: 全部失败 → 错误(降级链终点的真实反馈)
	p := NewPingCollector(PingTCP)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// RFC5737 测试网段, 本机不可路由
	if _, _, _, err := p.tcp(ctx, "192.0.2.55", ":1"); err == nil {
		t.Fatal("unreachable target should error")
	}
}

func TestResolve_IPLiteralAndFamilyMismatch(t *testing.T) {
	ip, err := resolve(context.Background(), "114.114.114.114", 4)
	if err != nil || ip != "114.114.114.114" {
		t.Fatalf("resolve = %s, %v", ip, err)
	}
	if _, err := resolve(context.Background(), "114.114.114.114", 6); err == nil {
		t.Fatal("v4 literal with v6 target should error")
	}
}

func fmtPort(p int) string { return ":" + itoa(p) }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
