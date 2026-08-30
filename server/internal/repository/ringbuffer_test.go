package repository

import (
	"sync"
	"testing"
)

func TestRingBuffer_PartialFill(t *testing.T) {
	r := NewRingBuffer[int](4)
	for i := 1; i <= 3; i++ {
		r.Push(i)
	}
	if r.Len() != 3 {
		t.Fatalf("len = %d, want 3", r.Len())
	}
	got := r.Snapshot()
	want := []int{1, 2, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot = %v, want %v", got, want)
		}
	}
}

func TestRingBuffer_WrapsAndOverwritesOldest(t *testing.T) {
	r := NewRingBuffer[int](4)
	for i := 1; i <= 6; i++ {
		r.Push(i)
	}
	if r.Len() != 4 {
		t.Fatalf("len = %d, want 4", r.Len())
	}
	got := r.Snapshot()
	want := []int{3, 4, 5, 6} // 最旧的 1,2 被覆盖
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot = %v, want %v", got, want)
		}
	}
	if latest := r.Latest(2); latest[0] != 5 || latest[1] != 6 {
		t.Fatalf("latest(2) = %v, want [5 6]", latest)
	}
}

func TestRingBuffer_LatestExceedsLen(t *testing.T) {
	r := NewRingBuffer[int](8)
	r.Push(1)
	if got := r.Latest(5); len(got) != 1 || got[0] != 1 {
		t.Fatalf("latest(5) = %v, want [1]", got)
	}
}

func TestRingBuffer_ClampAndCapacity(t *testing.T) {
	r := NewRingBuffer[int](0) // 非法容量钳制为 1
	if r.Capacity() != 1 {
		t.Fatalf("capacity = %d, want 1", r.Capacity())
	}
	r.Push(7)
	if r.Len() != 1 || r.Snapshot()[0] != 7 {
		t.Fatalf("len/snapshot mismatch")
	}
}

func TestRingBuffer_ConcurrentPushSnapshot(t *testing.T) {
	// 并发冒烟测试: push 与 snapshot 并发不 panic、长度守恒(设计文档 9.4 环形缓冲并发风险)
	r := NewRingBuffer[int](128)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				r.Push(w*1000 + i)
				_ = r.Snapshot()
			}
		}(w)
	}
	wg.Wait()
	if r.Len() != 128 {
		t.Fatalf("len = %d, want 128", r.Len())
	}
	if len(r.Snapshot()) != 128 {
		t.Fatal("snapshot length mismatch")
	}
}
