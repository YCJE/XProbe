package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testTracker(t *testing.T) (*Tracker, *time.Time) {
	t.Helper()
	tr, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local)
	cur := base
	tr.now = func() time.Time { return cur }
	return tr, &cur
}

func readState(t *testing.T, tr *Tracker) fileState {
	t.Helper()
	b, err := os.ReadFile(tr.path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st fileState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return st
}

func TestAccumulateAcrossRestart(t *testing.T) {
	tr, clock := testTracker(t)

	// First call establishes the baseline: no delta, nothing counted yet.
	out, err := tr.Update(*clock, 1000, 500)
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if out.Month != "2026-09" || out.RxBytes != 0 || out.TxBytes != 0 {
		t.Fatalf("first update out = %+v", out)
	}

	// Simulate a reboot: fresh tracker, same counters on disk now at 2000.
	*clock = clock.Add(time.Second)
	_ = tr.Close()
	tr2, err := Open(tr.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	tr2.now = tr.now

	out, err = tr2.Update(clock.Add(time.Second), 2000, 1500)
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	// delta = 2000-1000 = 1000 rx; 1500-500 = 1000 tx, accumulated on top of 0.
	if out.RxBytes != 1000 || out.TxBytes != 1000 {
		t.Fatalf("accumulate wrong: %+v", out)
	}
	// Close() must force-persist the cumulative values.
	_ = tr2.Close()
	st := readState(t, tr2)
	if st.CumulativeRx != 1000 || st.CumulativeTx != 1000 {
		t.Fatalf("persisted state wrong: %+v", st)
	}
}

func TestCounterResetOnReboot(t *testing.T) {
	tr, clock := testTracker(t)

	// Fresh tracker: first update only establishes the baseline (no delta).
	if _, err := tr.Update(*clock, 5000, 2500); err != nil {
		t.Fatalf("update: %v", err)
	}
	*clock = clock.Add(time.Minute) // past throttle, persists

	// Counters drop: machine rebooted with fresh /proc/net/dev.
	out, err := tr.Update(*clock, 300, 200)
	if err != nil {
		t.Fatalf("update after reset: %v", err)
	}
	// Deltas restart from the new counter values: 300 rx, 200 tx.
	if out.RxBytes != 300 || out.TxBytes != 200 {
		t.Fatalf("reset handling wrong: %+v", out)
	}
}

func TestMonthRolloverResetsAndPersists(t *testing.T) {
	tr, clock := testTracker(t)

	if _, err := tr.Update(*clock, 10000, 5000); err != nil {
		t.Fatalf("update: %v", err)
	}

	*clock = time.Date(2026, 10, 1, 0, 0, 1, 0, time.Local)
	out, err := tr.Update(*clock, 10500, 5200)
	if err != nil {
		t.Fatalf("update after rollover: %v", err)
	}
	if out.Month != "2026-10" {
		t.Fatalf("month = %s, want 2026-10", out.Month)
	}
	if out.RxBytes != 500 || out.TxBytes != 200 {
		t.Fatalf("rollover should reset cumulative: %+v", out)
	}
	// Rollover forces an immediate persist even inside the throttle window.
	st := readState(t, tr)
	if st.Month != "2026-10" || st.CumulativeRx != 500 {
		t.Fatalf("rollover not persisted: %+v", st)
	}
}

func TestThrottledPersist(t *testing.T) {
	tr, clock := testTracker(t)

	// First update persists the baseline immediately (durability default).
	if _, err := tr.Update(*clock, 1000, 500); err != nil {
		t.Fatalf("update: %v", err)
	}
	st := readState(t, tr)
	if st.LastRx != 1000 || st.CumulativeRx != 0 {
		t.Fatalf("baseline state wrong: %+v", st)
	}

	// Second update inside the throttle window: in-memory only.
	if _, err := tr.Update(clock.Add(time.Second), 1100, 600); err != nil {
		t.Fatalf("update: %v", err)
	}
	st = readState(t, tr)
	if st.LastRx != 1000 {
		t.Fatalf("expected no persist inside throttle window: %+v", st)
	}

	// Past the throttle window: deltas land on disk.
	if _, err := tr.Update(clock.Add(61*time.Second), 2000, 1000); err != nil {
		t.Fatalf("update: %v", err)
	}
	st = readState(t, tr)
	if st.CumulativeRx != 1000 || st.CumulativeTx != 500 || st.LastRx != 2000 {
		t.Fatalf("expected persist after throttle window: %+v", st)
	}
}
