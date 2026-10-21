package monitor

import (
	"context"
	"testing"
	"time"

	"probe/common/wire"
)

func TestReplacedConnectionCannotMarkAgentOffline(t *testing.T) {
	h := NewHub()
	_, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	_, cancelNew := context.WithCancel(context.Background())
	defer cancelNew()

	if prev := h.Attach(1, "old", cancelOld); prev != nil {
		t.Fatal("unexpected previous connection")
	}
	if prev := h.Attach(1, "new", cancelNew); prev == nil {
		t.Fatal("expected the old connection to be returned")
	}
	if h.Detach(1, "old") {
		t.Fatal("a replaced connection must not detach the current one")
	}
	if !h.Online(1) {
		t.Fatal("agent went offline while the new connection is live")
	}
	if !h.Detach(1, "new") {
		t.Fatal("the current connection did not detach")
	}
	if h.Online(1) {
		t.Fatal("agent stayed online after the current connection closed")
	}
}

func TestLatestReport(t *testing.T) {
	h := NewHub()
	if _, ok := h.Latest(1); ok {
		t.Fatal("empty hub returned a report")
	}
	report := wire.Report{Type: wire.TypeReport, Timestamp: time.Now().Unix(), Hostname: "x"}
	h.Store(1, report)
	got, ok := h.Latest(1)
	if !ok || got.Hostname != "x" {
		t.Fatalf("latest = %+v, %v", got, ok)
	}
}
