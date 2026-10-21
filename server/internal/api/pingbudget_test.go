package api

import (
	"testing"
	"time"
)

func TestPingBudget(t *testing.T) {
	var b pingBudget
	now := time.Unix(1000, 0)
	if !b.Allow(128, now) {
		t.Fatal("initial burst should be allowed")
	}
	if b.Allow(1, now) {
		t.Fatal("budget should be exhausted")
	}
	now = now.Add(time.Minute)
	if !b.Allow(64, now) {
		t.Fatal("one minute refill should allow one 64-target round")
	}
	if b.Allow(64, now.Add(time.Second)) {
		t.Fatal("second round in the same minute should be over budget")
	}
}
