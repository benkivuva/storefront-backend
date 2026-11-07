package redis

import (
	"context"
	"sync"
	"testing"
	"time"
)

func mustMem(t *testing.T, capacity int, window time.Duration) *MemoryLimiter {
	t.Helper()
	m, err := NewMemoryLimiter(capacity, window)
	if err != nil {
		t.Fatalf("NewMemoryLimiter: %v", err)
	}
	return m
}

func TestMemoryLimiter_AllowsUpToCapacity(t *testing.T) {
	m := mustMem(t, 5, time.Minute)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		d, err := m.Allow(ctx, "k", 1)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if !d.Allowed {
			t.Fatalf("iter %d: rejected", i)
		}
	}
	d, _ := m.Allow(ctx, "k", 1)
	if d.Allowed {
		t.Fatal("6th request should be rejected")
	}
	if d.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want > 0", d.RetryAfter)
	}
}

func TestMemoryLimiter_KeysAreIsolated(t *testing.T) {
	m := mustMem(t, 2, time.Minute)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, _ = m.Allow(ctx, "a", 1)
	}
	if d, _ := m.Allow(ctx, "a", 1); d.Allowed {
		t.Fatal("a should be exhausted")
	}
	if d, _ := m.Allow(ctx, "b", 1); !d.Allowed {
		t.Fatal("b should not be exhausted")
	}
}

func TestMemoryLimiter_ConcurrentExactlyCapacity(t *testing.T) {
	m := mustMem(t, 10, time.Minute)
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, _ := m.Allow(ctx, "hot", 1)
			if d.Allowed {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 10 {
		t.Fatalf("successes = %d, want exactly 10", successes)
	}
}

func TestMemoryLimiter_Refills(t *testing.T) {
	m := mustMem(t, 10, time.Second) // 10 tokens per second
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = m.Allow(ctx, "k", 1)
	}
	if d, _ := m.Allow(ctx, "k", 1); d.Allowed {
		t.Fatal("should be exhausted")
	}
	time.Sleep(200 * time.Millisecond) // ~2 tokens refill
	if d, _ := m.Allow(ctx, "k", 1); !d.Allowed {
		t.Fatal("should have refilled")
	}
}

func TestMemoryLimiter_CostExceedsCapacity(t *testing.T) {
	m := mustMem(t, 5, time.Minute)
	d, err := m.Allow(context.Background(), "k", 10)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatal("cost > capacity must be rejected")
	}
}

func TestMemoryLimiter_Sweep(t *testing.T) {
	m := mustMem(t, 5, time.Second)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = m.Allow(ctx, "k1", 1)
	}
	_, _ = m.Allow(ctx, "k2", 1) // k2 has full bucket
	time.Sleep(1100 * time.Millisecond)
	m.Sweep()
	m.mu.Lock()
	count := len(m.buckets)
	m.mu.Unlock()
	if count > 1 {
		t.Errorf("buckets remaining = %d, want ≤1 (refilled buckets removed)", count)
	}
}

func TestNewMemoryLimiter_InvalidArgs(t *testing.T) {
	if _, err := NewMemoryLimiter(0, time.Minute); err != ErrInvalidCapacity {
		t.Errorf("want ErrInvalidCapacity, got %v", err)
	}
	if _, err := NewMemoryLimiter(5, 0); err != ErrInvalidWindow {
		t.Errorf("want ErrInvalidWindow, got %v", err)
	}
}
