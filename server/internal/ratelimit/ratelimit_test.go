package ratelimit

import (
	"testing"
	"time"
)

func TestWindowLimitsPerKey(t *testing.T) {
	w := NewWindow(2, time.Minute)
	now := time.Unix(0, 0)
	if !w.AllowAt("a", now) {
		t.Fatal("first attempt should be allowed")
	}
	if !w.AllowAt("a", now) {
		t.Fatal("second attempt should be allowed")
	}
	if w.AllowAt("a", now) {
		t.Fatal("third attempt in the window should be refused")
	}
	if !w.AllowAt("b", now) {
		t.Fatal("another key should have its own budget")
	}
	if !w.AllowAt("a", now.Add(time.Minute)) {
		t.Fatal("a new window should allow again")
	}
}

func TestLimiterMapsAreBounded(t *testing.T) {
	w := NewWindow(5, time.Minute)
	now := time.Unix(0, 0)
	for i := 0; i < maxEntries+2000; i++ {
		w.AllowAt(string(rune(i)), now)
	}
	if len(w.seen) > maxEntries {
		t.Fatalf("window map grew to %d", len(w.seen))
	}
	th := NewLoginThrottle(100, 3, time.Minute)
	for i := 0; i < maxEntries+2000; i++ {
		th.FailureAt(string(rune(i)), now)
	}
	if len(th.failures) > maxEntries {
		t.Fatalf("failure map grew to %d", len(th.failures))
	}
}

func TestLoginThrottleLocksAfterFailures(t *testing.T) {
	th := NewLoginThrottle(100, 3, 15*time.Minute)
	now := time.Unix(0, 0)
	if !th.AllowAt("ip", now) {
		t.Fatal("first attempt should be allowed")
	}
	th.FailureAt("ip", now)
	th.FailureAt("ip", now)
	th.FailureAt("ip", now)
	if th.AllowAt("ip", now.Add(time.Second)) {
		t.Fatal("locked address was allowed")
	}
	if !th.AllowAt("ip", now.Add(16*time.Minute)) {
		t.Fatal("lockout should expire")
	}
	th.Success("ip")
	if !th.AllowAt("ip", now.Add(17*time.Minute)) {
		t.Fatal("success should clear failures")
	}
}
