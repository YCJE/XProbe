package api

import (
	"math"
	"time"

	"probe/common/wire"
)

// pingBudget caps how many ping entries one agent connection may persist.
// The agent sends at most wire.MaxPingEntries targets every 60 seconds, so a
// two-round burst is generous while still stopping a compromised agent from
// filling the database at frame-rate speed.
const pingBudgetBurst = float64(wire.MaxPingEntries * 2)
const pingBudgetPerSecond = float64(wire.MaxPingEntries) / 60

type pingBudget struct {
	tokens float64
	last   time.Time
}

func (b *pingBudget) Allow(entries int, now time.Time) bool {
	if b.last.IsZero() {
		b.tokens = pingBudgetBurst
		b.last = now
	}
	b.tokens = math.Min(pingBudgetBurst, b.tokens+now.Sub(b.last).Seconds()*pingBudgetPerSecond)
	b.last = now
	if entries < 0 || float64(entries) > b.tokens {
		return false
	}
	b.tokens -= float64(entries)
	return true
}
