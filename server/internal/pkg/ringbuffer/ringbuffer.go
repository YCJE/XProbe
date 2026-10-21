// Package ringbuffer provides a fixed-capacity, mutex-guarded ring buffer used
// for per-agent realtime metric retention (§5.3: 3s/point × 3h = 3600 points).
package ringbuffer

import "sync"

// Buffer stores the most recent `size` items in insertion order.
type Buffer[T any] struct {
	mu    sync.Mutex
	buf   []T
	head  int // next write position
	count int
}

// New allocates a buffer with the given capacity.
func New[T any](size int) *Buffer[T] {
	return &Buffer[T]{buf: make([]T, size)}
}

// Size returns the fixed capacity.
func (b *Buffer[T]) Size() int {
	return len(b.buf)
}

// Len returns the number of currently stored items (≤ Size).
func (b *Buffer[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// Push appends an item, overwriting the oldest one when full.
func (b *Buffer[T]) Push(v T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) == 0 {
		return
	}
	b.buf[b.head] = v
	b.head = (b.head + 1) % len(b.buf)
	if b.count < len(b.buf) {
		b.count++
	}
}

// Snapshot returns a copy of all items ordered oldest → newest.
func (b *Buffer[T]) Snapshot() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rangeLocked(0, b.count)
}

// Latest returns a copy of the most recent n items ordered oldest → newest.
// Fewer items are returned if the buffer holds less than n.
func (b *Buffer[T]) Latest(n int) []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.count {
		n = b.count
	}
	if n <= 0 {
		return nil
	}
	return b.rangeLocked(b.count-n, b.count)
}

// rangeLocked copies items [from, to) in logical order. Caller must hold mu.
func (b *Buffer[T]) rangeLocked(from, to int) []T {
	out := make([]T, to-from)
	start := (b.head - b.count + len(b.buf) + from) % len(b.buf)
	for i := 0; i < to-from; i++ {
		out[i] = b.buf[(start+i)%len(b.buf)]
	}
	return out
}
