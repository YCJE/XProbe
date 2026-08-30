package repository

import "sync"

// RingBuffer 固定容量环形缓冲, 每 Agent 一个实例(设计文档 5.3: 3秒/点 × 3小时 = 3600点)。
// 吸收高频实时写入, 覆盖最旧数据; Snapshot 返回旧→新顺序副本。
type RingBuffer[T any] struct {
	mu   sync.RWMutex
	buf  []T
	head int  // 下一个写入位置
	full bool // 已写满一轮
}

func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer[T]{buf: make([]T, capacity)}
}

func (r *RingBuffer[T]) Capacity() int {
	return len(r.buf)
}

// Len 返回当前有效元素数。
func (r *RingBuffer[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.full {
		return len(r.buf)
	}
	return r.head
}

// Push 写入一个点, 缓冲满时覆盖最旧数据。
func (r *RingBuffer[T]) Push(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = v
	r.head++
	if r.head == len(r.buf) {
		r.head = 0
		r.full = true
	}
}

// Snapshot 返回旧→新顺序的数据副本(调用方可安全持有)。
// 注意: 内部不得调用 Len()(同持读锁再取读锁, 写者等待时会死锁), 长度就地计算。
func (r *RingBuffer[T]) Snapshot() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := len(r.buf)
	count := r.head
	if r.full {
		count = n
	}
	out := make([]T, 0, count)
	if !r.full {
		out = append(out, r.buf[:r.head]...)
		return out
	}
	out = append(out, r.buf[r.head:n]...) // 最旧段
	out = append(out, r.buf[:r.head]...)  // 最新段
	return out
}

// Latest 返回最近 n 个点(旧→新); 不足 n 个时返回全部。
func (r *RingBuffer[T]) Latest(n int) []T {
	s := r.Snapshot()
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
