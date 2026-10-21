package ringbuffer

import (
	"sync"
	"testing"
)

func TestPushAndSnapshot(t *testing.T) {
	b := New[int](3)
	b.Push(1)
	b.Push(2)
	got := b.Snapshot()
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("unexpected snapshot: %v", got)
	}
}

func TestOverwriteOldest(t *testing.T) {
	b := New[int](3)
	for i := 1; i <= 5; i++ {
		b.Push(i)
	}
	got := b.Snapshot()
	want := []int{3, 4, 5}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
	if b.Len() != 3 || b.Size() != 3 {
		t.Fatalf("len/size mismatch: %d/%d", b.Len(), b.Size())
	}
}

func TestLatest(t *testing.T) {
	b := New[int](5)
	for i := 1; i <= 4; i++ {
		b.Push(i)
	}
	got := b.Latest(2)
	if len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("expected [3 4], got %v", got)
	}
	if got := b.Latest(100); len(got) != 4 {
		t.Fatalf("expected clamped to 4, got %v", got)
	}
	if got := b.Latest(0); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestConcurrentPushAndRead(t *testing.T) {
	b := New[int](64)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				b.Push(w*1000 + i)
				_ = b.Snapshot()
				_ = b.Latest(10)
			}
		}(w)
	}
	wg.Wait()
	if b.Len() != 64 {
		t.Fatalf("expected full buffer, got %d", b.Len())
	}
}

func TestZeroCapacitySafe(t *testing.T) {
	b := New[int](0)
	b.Push(1) // must not panic
	if b.Len() != 0 {
		t.Fatalf("expected empty, got %d", b.Len())
	}
}
