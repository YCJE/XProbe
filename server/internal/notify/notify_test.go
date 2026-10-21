package notify

import (
	"net/netip"
	"testing"
)

func TestBlockedAddresses(t *testing.T) {
	blockedList := []string{"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "fc00::1", "fe80::1", "0.0.0.0", "0.1.2.3",
		"100.64.0.1", "198.18.0.1", "192.0.0.1"}
	for _, raw := range blockedList {
		if !blocked(netip.MustParseAddr(raw)) {
			t.Fatalf("%s should be blocked", raw)
		}
	}
	if blocked(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public address was blocked")
	}
}

func TestResolveBlocksLocalWebhook(t *testing.T) {
	if _, err := resolvePublic(t.Context(), "127.0.0.1"); err == nil {
		t.Fatal("loopback address was accepted")
	}
	if _, err := resolvePublic(t.Context(), "::1"); err == nil {
		t.Fatal("IPv6 loopback address was accepted")
	}
	if err := ValidateChannel("webhook", map[string]any{"url": "not-a-url"}); err == nil {
		t.Fatal("invalid webhook was accepted")
	}
	if err := ValidateChannel("telegram", map[string]any{"bot_token": "x", "chat_id": "1"}); err != nil {
		t.Fatalf("valid telegram rejected: %v", err)
	}
}

func TestWebhookHeaders(t *testing.T) {
	got, err := webhookHeaders(`{"Authorization":"Bearer token","X-Trace":"abc"}`)
	if err != nil || got["Authorization"] != "Bearer token" || got["X-Trace"] != "abc" {
		t.Fatalf("valid headers rejected: %v %v", got, err)
	}
	if _, err := webhookHeaders(`{"X-Test":"a\r\nX-Injected: 1"}`); err == nil {
		t.Fatal("CRLF header value was accepted")
	}
	if _, err := webhookHeaders(`{"Bad Header":"x"}`); err == nil {
		t.Fatal("invalid header name was accepted")
	}
	if _, err := webhookHeaders(`{"Host":"evil.example"}`); err == nil {
		t.Fatal("Host override was accepted")
	}
	if _, err := webhookHeaders(`["not","an","object"]`); err == nil {
		t.Fatal("non-object headers were accepted")
	}
	if err := ValidateChannel("webhook", map[string]any{
		"url": "https://example.com/hook", "headers": `{"Bad Header":"x"}`,
	}); err == nil {
		t.Fatal("ValidateChannel accepted invalid headers")
	}
}
