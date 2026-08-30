package pkg

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsPrivateIP(t *testing.T) {
	cases := map[string]bool{
		"10.1.2.3": true, "172.16.0.1": true, "192.168.1.1": true, "127.0.0.1": true,
		"169.254.1.1": true, "0.0.0.0": true, "100.64.0.1": true, "::1": true,
		"fd00::1": true, "fe80::1": true,
		"8.8.8.8": false, "1.1.1.1": false, "2606:4700::1111": false,
	}
	for ipStr, want := range cases {
		if got := IsPrivateIP(net.ParseIP(ipStr)); got != want {
			t.Fatalf("IsPrivateIP(%s) = %v, want %v", ipStr, got, want)
		}
	}
}

func TestCheckURLSchemeAndHost(t *testing.T) {
	if _, err := CheckURLSchemeAndHost("https://hooks.example.com/x"); err != nil {
		t.Fatalf("https ok: %v", err)
	}
	for _, bad := range []string{"file:///etc/passwd", "ftp://x", "gopher://x", "https://"} {
		if _, err := CheckURLSchemeAndHost(bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}

func TestSafeClient_BlocksLoopback(t *testing.T) {
	// httptest 监听 127.0.0.1: SSRF 防护必须拦截(设计文档 7.4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	client := NewSafeClient(5 * time.Second)
	err := client.PostJSON(context.Background(), srv.URL, []byte(`{}`))
	if err == nil {
		t.Fatal("request to loopback must be blocked (SSRF)")
	}
}

func TestSafeClient_PublicHost(t *testing.T) {
	// 无法在测试中直连真实公网; 验证公共地址解析路径不误报:
	// 用 httptest 的地址 + 手动 DNS 覆盖不可行, 此处仅验证 PostJSON 对不可达公网地址报连接错误而非 SSRF 拦截
	client := NewSafeClient(3 * time.Second)
	err := client.PostJSON(context.Background(), "https://203.0.113.7/hook", []byte(`{}`))
	if err != nil && !isNetworkErr(err) {
		t.Fatalf("expected network error, got: %v", err)
	}
}

func isNetworkErr(err error) bool {
	return err != nil && !containsStr(err.Error(), "ssrf: request to private")
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
