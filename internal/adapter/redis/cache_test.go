package redis

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

func TestCache_GetOrFetch(t *testing.T) {
	c := setupRedis(t)
	cache := NewCache(c, "test")
	ctx := context.Background()

	// 1. Fetch a missing key
	var fetchCount int32
	fetchFn := func() ([]byte, error) {
		atomic.AddInt32(&fetchCount, 1)
		time.Sleep(50 * time.Millisecond) // Simulate slow DB
		return []byte("hello"), nil
	}

	// 2. Fire 100 concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := cache.GetOrFetch(ctx, "key1", time.Minute, fetchFn)
			if err != nil {
				t.Errorf("GetOrFetch: %v", err)
			}
			if string(val) != "hello" {
				t.Errorf("expected hello, got %q", val)
			}
		}()
	}
	wg.Wait()

	if fetchCount != 1 {
		t.Fatalf("DB was hit %d times, want exactly 1 (singleflight failed)", fetchCount)
	}

	// 3. Next request should be a cache hit (0 DB calls)
	_, _ = cache.GetOrFetch(ctx, "key1", time.Minute, fetchFn)
	if fetchCount != 1 {
		t.Fatalf("DB was hit %d times, want 1 (cache hit failed)", fetchCount)
	}
}

func TestCache_NegativeCaching(t *testing.T) {
	c := setupRedis(t)
	cache := NewCache(c, "test")
	ctx := context.Background()

	var fetchCount int32
	fetchFn := func() ([]byte, error) {
		atomic.AddInt32(&fetchCount, 1)
		return nil, domain.ErrNotFound
	}

	// First request fails, sets negative cache
	_, err := cache.GetOrFetch(ctx, "bad-key", time.Minute, fetchFn)
	if err != domain.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// Second request should hit negative cache and NOT call fetchFn
	_, err = cache.GetOrFetch(ctx, "bad-key", time.Minute, fetchFn)
	if err != domain.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	if fetchCount != 1 {
		t.Fatalf("DB was hit %d times, want 1", fetchCount)
	}
}
