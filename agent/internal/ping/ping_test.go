package ping

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"probe/common/wire"
)

func TestSplitTarget(t *testing.T) {
	cases := []struct{ raw, host, port string }{
		{"1.1.1.1:443", "1.1.1.1", "443"},
		{"[2001:db8::1]:8443", "2001:db8::1", "8443"},
		{"2001:db8::1", "2001:db8::1", "443"},
		{"example.com", "example.com", "443"},
	}
	for _, c := range cases {
		host, port := splitTarget(c.raw, "443")
		if host != c.host || port != c.port {
			t.Fatalf("splitTarget(%q) = %q,%q", c.raw, host, port)
		}
	}
}

func TestTCPPing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	stat := Run(context.Background(), wire.PingTarget{
		Target: listener.Addr().String(), Name: "local", Protocol: "tcp", IPVersion: 4,
	}, "auto")
	if stat.Method != "tcp" || stat.PacketsRecv != tcpCount || stat.Loss != 0 {
		t.Fatalf("unexpected stat: %+v", stat)
	}
	if stat.AvgLatency <= 0 {
		t.Fatalf("latency = %v", stat.AvgLatency)
	}
}

func TestHTTPPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stat := Run(ctx, wire.PingTarget{
		Target: server.URL, Name: "http", Protocol: "http", IPVersion: 4,
	}, "auto")
	if stat.Method != "http" || stat.PacketsRecv != httpCount || stat.Loss != 0 {
		t.Fatalf("unexpected stat: %+v", stat)
	}
}

func TestUnreachableTCPIsLoss(t *testing.T) {
	// Port 1 on loopback is closed in the test environment.
	stat := Run(context.Background(), wire.PingTarget{
		Target: "127.0.0.1:1", Name: "closed", Protocol: "tcp", IPVersion: 4,
	}, "tcp")
	if stat.Loss != 100 || stat.PacketsRecv != 0 {
		t.Fatalf("expected 100%% loss, got %+v", stat)
	}
}
