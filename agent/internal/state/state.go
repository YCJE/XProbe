// Package state persists monthly traffic counters across agent restarts
// (§4.4: /proc/net/dev counters reset on reboot, so the agent keeps its own
// cumulative values). Writes are throttled to 60s to avoid SSD wear.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"probe/common/wire"
)

const (
	monthFormat = "2006-01"

	// persistThrottle is the minimum interval between disk writes (§4.4).
	persistThrottle = 60 * time.Second
)

type fileState struct {
	Month        string `json:"month"`
	CumulativeRx uint64 `json:"cumulative_rx"`
	CumulativeTx uint64 `json:"cumulative_tx"`
	LastRx       uint64 `json:"last_rx"`
	LastTx       uint64 `json:"last_tx"`
}

// Tracker is safe for concurrent use.
type Tracker struct {
	path string

	mu          sync.Mutex
	st          fileState
	loaded      bool // state file existed at Open (cumulative values restored)
	inited      bool // at least one Update done (counter baseline established)
	lastPersist time.Time
	now         func() time.Time
}

// Open loads the state file (or starts fresh) and prepares the tracker.
func Open(path string) (*Tracker, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
	}

	tr := &Tracker{path: path, now: time.Now}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &tr.st); err != nil {
			return nil, fmt.Errorf("parse state file %s: %w", path, err)
		}
		tr.loaded = true
		// Persisted Last counters are a valid baseline: deltas count immediately.
		tr.inited = tr.st.Month != ""
	case errors.Is(err, os.ErrNotExist):
		// fresh start
	default:
		return nil, fmt.Errorf("read state file %s: %w", path, err)
	}
	tr.lastPersist = tr.now()
	return tr, nil
}

// Update folds current interface counters into the monthly cumulative totals.
// totalRx/totalTx must exclude loopback. The month field and rollover use the
// agent-local timezone (§4.4). Disk writes happen at most once per throttle
// window, immediately on month rollover, and on Close.
func (t *Tracker) Update(now time.Time, totalRx, totalTx uint64) (wire.TrafficMonthly, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var dRx, dTx uint64
	if t.inited {
		dRx = deltaSince(t.st.LastRx, totalRx)
		dTx = deltaSince(t.st.LastTx, totalTx)
	}

	cur := now.Format(monthFormat)
	rolled := cur != t.st.Month
	if rolled {
		t.st.Month = cur
		t.st.CumulativeRx, t.st.CumulativeTx = 0, 0
	}
	t.st.CumulativeRx += dRx
	t.st.CumulativeTx += dTx
	t.st.LastRx, t.st.LastTx = totalRx, totalTx
	t.inited = true

	out := wire.TrafficMonthly{
		Month:   t.st.Month,
		RxBytes: t.st.CumulativeRx,
		TxBytes: t.st.CumulativeTx,
	}

	var err error
	if rolled || now.Sub(t.lastPersist) >= persistThrottle {
		err = t.persistLocked()
		t.lastPersist = now
	}
	return out, err
}

// Close forces a final persist.
func (t *Tracker) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.persistLocked(); err != nil {
		return err
	}
	t.lastPersist = t.now()
	return nil
}

// deltaSince computes counter growth; a shrinking counter means the machine
// rebooted, so the new counter value becomes the delta from zero.
func deltaSince(last, cur uint64) uint64 {
	if cur >= last {
		return cur - last
	}
	return cur
}

func (t *Tracker) persistLocked() error {
	b, err := json.Marshal(t.st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, t.path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}
