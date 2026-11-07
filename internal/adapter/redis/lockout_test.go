package redis

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

var (
	lockUser1 = domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	lockUser2 = domain.ID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}
)

var testSchedule = []BackoffStep{
	{MinFailures: 2, Wait: 100 * time.Millisecond},
	{MinFailures: 4, Wait: 500 * time.Millisecond},
	{MinFailures: 6, Wait: 2 * time.Second},
}

func mustLockout(t *testing.T, c *goredis.Client) *LockoutStore {
	t.Helper()
	ls, err := NewLockoutStore(c, testSchedule, time.Minute)
	if err != nil {
		t.Fatalf("NewLockoutStore: %v", err)
	}
	return ls
}

func TestLockout_FreshNoBackoff(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)

	w, err := ls.Backoff(context.Background(), lockUser1, "1.2.3.0/24")
	if err != nil {
		t.Fatalf("Backoff: %v", err)
	}
	if w != 0 {
		t.Fatalf("fresh key: wait = %v, want 0", w)
	}
}

func TestLockout_BelowThresholdNoBackoff(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	w, err := ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if w != 0 {
		t.Fatalf("1st failure: wait = %v, want 0", w)
	}
	w, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	if w != 100*time.Millisecond {
		t.Fatalf("2nd failure: wait = %v, want 100ms", w)
	}
}

func TestLockout_BackoffPersists(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")

	w, err := ls.Backoff(ctx, lockUser1, "1.2.3.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if w <= 0 || w > 100*time.Millisecond {
		t.Fatalf("wait = %v, want (0,100ms]", w)
	}
}

func TestLockout_BackoffExpires(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")

	time.Sleep(150 * time.Millisecond)

	w, _ := ls.Backoff(ctx, lockUser1, "1.2.3.0/24")
	if w != 0 {
		t.Fatalf("wait = %v after expiry, want 0", w)
	}
}

func TestLockout_Escalation(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	w, _ := ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24") // count 1
	if w != 0 {
		t.Errorf("F1: wait = %v, want 0", w)
	}
	w, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24") // count 2 → tier 1
	if w != 100*time.Millisecond {
		t.Errorf("F2: wait = %v, want 100ms", w)
	}
	time.Sleep(150 * time.Millisecond)
	w, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24") // count 3 → tier 1
	if w != 100*time.Millisecond {
		t.Errorf("F3: wait = %v, want 100ms", w)
	}
	time.Sleep(150 * time.Millisecond)
	w, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24") // count 4 → tier 2
	if w != 500*time.Millisecond {
		t.Errorf("F4: wait = %v, want 500ms", w)
	}
	time.Sleep(550 * time.Millisecond)
	w, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24") // count 5 → tier 2
	if w != 500*time.Millisecond {
		t.Errorf("F5: wait = %v, want 500ms", w)
	}
	time.Sleep(550 * time.Millisecond)
	w, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24") // count 6 → tier 3
	if w != 2*time.Second {
		t.Errorf("F6: wait = %v, want 2s", w)
	}
}

func TestLockout_ResetClears(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	if w, _ := ls.Backoff(ctx, lockUser1, "1.2.3.0/24"); w <= 0 {
		t.Fatal("expected active backoff before reset")
	}

	if err := ls.Reset(ctx, lockUser1, "1.2.3.0/24"); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if w, _ := ls.Backoff(ctx, lockUser1, "1.2.3.0/24"); w != 0 {
		t.Fatalf("wait after reset = %v, want 0", w)
	}

	if w, _ := ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24"); w != 0 {
		t.Fatalf("post-reset failure: wait = %v, want 0", w)
	}
}

func TestLockout_KeyIsolationByUser(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	}
	if w, _ := ls.Backoff(ctx, lockUser2, "1.2.3.0/24"); w != 0 {
		t.Fatalf("user2 should have no backoff, got %v", w)
	}
}

func TestLockout_KeyIsolationByPrefix(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	}
	if w, _ := ls.Backoff(ctx, lockUser1, "9.9.9.0/24"); w != 0 {
		t.Fatalf("different prefix should have no backoff, got %v", w)
	}
}

func TestLockout_Concurrent(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	var wg sync.WaitGroup
	const N = 20
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24"); err != nil {
				t.Errorf("RecordFailedAttempt: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	count, err := c.HGet(ctx, lockoutKey(lockUser1, "1.2.3.0/24"), "count").Int64()
	if err != nil {
		t.Fatal(err)
	}
	if count != int64(N) {
		t.Fatalf("count = %d, want %d (Lua must serialize increments)", count, N)
	}

	w, _ := ls.Backoff(ctx, lockUser1, "1.2.3.0/24")
	if w <= 0 || w > 2*time.Second {
		t.Fatalf("wait = %v, want (0,2s]", w)
	}
}

func TestLockout_TTLSets(t *testing.T) {
	c := setupRedis(t)
	ls := mustLockout(t, c)
	ctx := context.Background()

	_, _ = ls.RecordFailedAttempt(ctx, lockUser1, "1.2.3.0/24")
	ttl, err := c.TTL(ctx, lockoutKey(lockUser1, "1.2.3.0/24")).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 {
		t.Fatalf("TTL = %v, want > 0", ttl)
	}
}

func TestLockout_RedisDown(t *testing.T) {
	c := goredis.NewClient(&goredis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
	})
	defer func() { _ = c.Close() }()
	ls := mustLockout(t, c)

	if _, err := ls.Backoff(context.Background(), lockUser1, "1.2.3.0/24"); err == nil {
		t.Fatal("expected error on backoff when Redis unreachable")
	}
	if _, err := ls.RecordFailedAttempt(context.Background(), lockUser1, "1.2.3.0/24"); err == nil {
		t.Fatal("expected error on record when Redis unreachable")
	}
	if err := ls.Reset(context.Background(), lockUser1, "1.2.3.0/24"); err == nil {
		t.Fatal("expected error on reset when Redis unreachable")
	}
}

func TestNewLockoutStore_InvalidArgs(t *testing.T) {
	c := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
	defer func() { _ = c.Close() }()

	cases := []struct {
		name     string
		schedule []BackoffStep
		ttl      time.Duration
		wantErr  error
	}{
		{"empty schedule", nil, time.Minute, ErrEmptySchedule},
		{"non-increasing threshold", []BackoffStep{{2, time.Second}, {2, time.Second}}, time.Minute, ErrInvalidThreshold},
		{"negative threshold", []BackoffStep{{-1, time.Second}}, time.Minute, ErrInvalidThreshold},
		{"zero wait", []BackoffStep{{2, 0}}, time.Minute, ErrInvalidWait},
		{"zero ttl", []BackoffStep{{2, time.Second}}, 0, ErrInvalidRecordTTL},
		{"ttl below max wait", []BackoffStep{{2, time.Minute}}, 30 * time.Second, ErrInvalidRecordTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewLockoutStore(c, tc.schedule, tc.ttl); !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}
