// Package ratelimit provides small in-memory limiters for login and agent
// registration. They are deliberately per-process: one hub, one admin.
package ratelimit

import (
	"sync"
	"time"
)

const maxEntries = 8192

type Window struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	seen   map[string]*windowEntry
}

type windowEntry struct {
	start time.Time
	count int
}

func NewWindow(limit int, window time.Duration) *Window {
	return &Window{limit: limit, window: window, now: time.Now, seen: map[string]*windowEntry{}}
}

// AllowAt is the deterministic form used by tests.
func (w *Window) AllowAt(key string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.seen[key]
	if !ok || now.Sub(e.start) >= w.window {
		if len(w.seen) >= maxEntries {
			w.pruneLocked(now)
		}
		w.seen[key] = &windowEntry{start: now, count: 1}
		return true
	}
	if e.count >= w.limit {
		return false
	}
	e.count++
	return true
}

func (w *Window) Allow(key string) bool {
	return w.AllowAt(key, w.now())
}

func (w *Window) pruneLocked(now time.Time) {
	for key, entry := range w.seen {
		if now.Sub(entry.start) >= w.window {
			delete(w.seen, key)
		}
	}
	for key := range w.seen {
		if len(w.seen) < maxEntries {
			return
		}
		delete(w.seen, key)
	}
}

// LoginThrottle combines a per-minute attempt window with a failure lockout.
type LoginThrottle struct {
	mu          sync.Mutex
	window      *Window
	maxFailures int
	lockout     time.Duration
	now         func() time.Time
	failures    map[string]*failureEntry
}

type failureEntry struct {
	count   int
	locked  time.Time
	resetAt time.Time
}

func NewLoginThrottle(attemptsPerMinute, maxFailures int, lockout time.Duration) *LoginThrottle {
	return &LoginThrottle{
		window:      NewWindow(attemptsPerMinute, time.Minute),
		maxFailures: maxFailures,
		lockout:     lockout,
		now:         time.Now,
		failures:    map[string]*failureEntry{},
	}
}

// AllowAt records an attempt. It returns false when the address is locked or
// over its per-minute attempt budget.
func (t *LoginThrottle) AllowAt(key string, now time.Time) bool {
	t.mu.Lock()
	if f, ok := t.failures[key]; ok && now.Before(f.locked) {
		t.mu.Unlock()
		return false
	}
	t.mu.Unlock()
	return t.window.AllowAt(key, now)
}

func (t *LoginThrottle) Allow(key string) bool {
	return t.AllowAt(key, t.now())
}

// Failure records a failed password check. Ten failures lock the address for
// 15 minutes; a successful login clears the failure count.
func (t *LoginThrottle) Failure(key string) {
	t.FailureAt(key, t.now())
}

func (t *LoginThrottle) FailureAt(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.failures[key]
	if !ok || now.After(f.resetAt.Add(15*time.Minute)) {
		if !ok && len(t.failures) >= maxEntries {
			t.pruneFailuresLocked(now)
		}
		f = &failureEntry{resetAt: now}
		t.failures[key] = f
	}
	f.count++
	if f.count >= t.maxFailures {
		f.locked = now.Add(t.lockout)
		f.count = 0
	}
}

func (t *LoginThrottle) Success(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, key)
}

func (t *LoginThrottle) pruneFailuresLocked(now time.Time) {
	for key, entry := range t.failures {
		if now.After(entry.locked) && now.After(entry.resetAt.Add(15*time.Minute)) {
			delete(t.failures, key)
		}
	}
	for key := range t.failures {
		if len(t.failures) < maxEntries {
			return
		}
		delete(t.failures, key)
	}
}
