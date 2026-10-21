package proxy

import (
	"net/http/httptest"
	"testing"
)

func TestNoneIgnoresCloudflareHeaders(t *testing.T) {
	r, err := New(ModeNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("CF-Connecting-IP", "203.0.113.5")
	req.Header.Set("CF-Visitor", `{"scheme":"https"}`)
	got := r.Resolve(req)
	if got.Trusted || got.IP.String() != "192.0.2.10" || got.Scheme != "http" {
		t.Fatalf("unexpected info: %+v", got)
	}
}

func TestTunnelTrustsOnlyLoopback(t *testing.T) {
	r, err := New(ModeTunnel, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("CF-Connecting-IP", "203.0.113.5")
	req.Header.Set("CF-Visitor", `{"scheme":"https"}`)
	got := r.Resolve(req)
	if !got.Trusted || got.IP.String() != "203.0.113.5" || got.Scheme != "https" {
		t.Fatalf("unexpected tunnel info: %+v", got)
	}

	req.RemoteAddr = "198.51.100.7:1234"
	got = r.Resolve(req)
	if got.Trusted || got.IP.String() != "198.51.100.7" {
		t.Fatalf("non-loopback peer must not be trusted: %+v", got)
	}
}

func TestProxyRequiresConfiguredCIDR(t *testing.T) {
	r, err := New(ModeProxy, []string{"173.245.48.0/20"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.RemoteAddr = "173.245.48.9:443"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-Proto", "https")
	got := r.Resolve(req)
	if !got.Trusted || got.IP.String() != "203.0.113.9" || got.Scheme != "https" {
		t.Fatalf("unexpected proxy info: %+v", got)
	}

	req.RemoteAddr = "203.0.113.10:443"
	got = r.Resolve(req)
	if got.Trusted {
		t.Fatalf("peer outside the CIDR must not be trusted: %+v", got)
	}
}
