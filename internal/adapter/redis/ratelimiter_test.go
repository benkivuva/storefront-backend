package redis

import (
	"context"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupRedis(t *testing.T) *goredis.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	client := goredis.NewClient(&goredis.Options{
		Addr: host + ":" + port.Port(),
	})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return client
}

func mustLimiter(t *testing.T, c *goredis.Client, capacity int, window time.Duration) *Limiter {
	t.Helper()
	l, err := NewLimiter(c, capacity, window)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	return l
}

func TestLimiter_AllowsUpToCapacity(t *testing.T) {
	c := setupRedis(t)
	l := mustLimiter(t, c, 5, time.Minute)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		d, err := l.Allow(ctx, "k", 1)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if !d.Allowed {
			t.Fatalf("iter %d: rejected", i)
		}
	}
	d, _ := l.Allow(ctx, "k", 1)
	if d.Allowed {
		t.Fatal("6th request should be rejected")
	}
	if d.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want > 0", d.RetryAfter)
	}
}

func TestLimiter_KeysIsolated(t *testing.T) {
	c := setupRedis(t)
	l := mustLimiter(t, c, 2, time.Minute)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, _ = l.Allow(ctx, "a", 1)
	}
	if d, _ := l.Allow(ctx, "a", 1); d.Allowed {
		t.Fatal("a should be exhausted")
	}
	if d, _ := l.Allow(ctx, "b", 1); !d.Allowed {
		t.Fatal("b should not be exhausted")
	}
}

// Critical test: 100 concurrent requests against a 10-token bucket must
// yield exactly 10 successes. If the Lua script isn't atomic, some
// requests will race past the limit.
func TestLimiter_ConcurrentExactlyCapacity(t *testing.T) {
	c := setupRedis(t)
	l := mustLimiter(t, c, 10, time.Minute)
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	start := make(chan struct{})
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			d, err := l.Allow(ctx, "hot", 1)
			if err != nil {
				t.Errorf("concurrent Allow: %v", err)
				return
			}
			if d.Allowed {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if successes != 10 {
		t.Fatalf("successes = %d, want exactly 10 (Lua atomicity)", successes)
	}
}

func TestLimiter_Refills(t *testing.T) {
	c := setupRedis(t)
	l := mustLimiter(t, c, 10, time.Second) // 10 tokens per second
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = l.Allow(ctx, "k", 1)
	}
	if d, _ := l.Allow(ctx, "k", 1); d.Allowed {
		t.Fatal("should be exhausted")
	}
	time.Sleep(200 * time.Millisecond)
	if d, _ := l.Allow(ctx, "k", 1); !d.Allowed {
		t.Fatal("should have refilled")
	}
}

func TestLimiter_CostExceedsCapacity(t *testing.T) {
	c := setupRedis(t)
	l := mustLimiter(t, c, 5, time.Minute)
	d, err := l.Allow(context.Background(), "k", 10)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatal("cost > capacity must be rejected")
	}
}

func TestLimiter_TTLSets(t *testing.T) {
	c := setupRedis(t)
	l := mustLimiter(t, c, 5, time.Minute)
	ctx := context.Background()
	_, _ = l.Allow(ctx, "ttl-key", 1)
	ttl, err := c.TTL(ctx, "ttl-key").Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 {
		t.Fatalf("TTL not set (got %v)", ttl)
	}
}

func TestLimiter_RedisDown(t *testing.T) {
	c := goredis.NewClient(&goredis.Options{
		Addr:        "127.0.0.1:1", // nothing listening
		DialTimeout: 100 * time.Millisecond,
	})
	defer func() { _ = c.Close() }()
	l := mustLimiter(t, c, 5, time.Minute)
	_, err := l.Allow(context.Background(), "k", 1)
	if err == nil {
		t.Fatal("expected error when Redis is unreachable")
	}
}

func TestNewLimiter_InvalidArgs(t *testing.T) {
	c := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
	defer func() { _ = c.Close() }()
	if _, err := NewLimiter(c, 0, time.Minute); err != ErrInvalidCapacity {
		t.Errorf("want ErrInvalidCapacity, got %v", err)
	}
	if _, err := NewLimiter(c, 5, 0); err != ErrInvalidWindow {
		t.Errorf("want ErrInvalidWindow, got %v", err)
	}
}
