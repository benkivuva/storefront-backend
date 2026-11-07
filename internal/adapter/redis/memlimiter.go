package redis

import (
	"context"
	"sync"
	"time"
)

// MemoryLimiter is an in-process token bucket used as the fail-closed
// fallback when Redis is unavailable. Each pod has its own counters, so
// effective cluster-wide limits multiply by pod count — acceptable as a
// degraded-mode trade-off, not a substitute for the Redis limiter.
type MemoryLimiter struct {
	capacity     int
	refillPerSec float64
	mu           sync.Mutex
	buckets      map[string]*bucket
}

type bucket struct {
	tokens float64
	ts     time.Time
}

func NewMemoryLimiter(capacity int, window time.Duration) (*MemoryLimiter, error) {
	if capacity <= 0 {
		return nil, ErrInvalidCapacity
	}
	if window <= 0 {
		return nil, ErrInvalidWindow
	}
	return &MemoryLimiter{
		capacity:     capacity,
		refillPerSec: float64(capacity) / window.Seconds(),
		buckets:      make(map[string]*bucket),
	}, nil
}

func (m *MemoryLimiter) Allow(_ context.Context, key string, cost int) (Decision, error) {
	if cost < 1 {
		cost = 1
	}
	if cost > m.capacity {
		return Decision{Allowed: false}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(m.capacity), ts: now}
		m.buckets[key] = b
	}

	elapsed := now.Sub(b.ts).Seconds()
	if elapsed > 0 {
		b.tokens = minF(float64(m.capacity), b.tokens+elapsed*m.refillPerSec)
		b.ts = now
	}

	if b.tokens >= float64(cost) {
		b.tokens -= float64(cost)
		return Decision{Allowed: true, Remaining: int(b.tokens)}, nil
	}

	needed := float64(cost) - b.tokens
	retry := time.Duration(needed / m.refillPerSec * float64(time.Second)).Round(time.Second)
	return Decision{Allowed: false, Remaining: int(b.tokens), RetryAfter: retry}, nil
}

// Sweep removes buckets whose tokens have refilled to full — they carry no
// state that matters, so deleting them is safe and keeps the map bounded.
func (m *MemoryLimiter) Sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, b := range m.buckets {
		if now.Sub(b.ts).Seconds()*m.refillPerSec+b.tokens >= float64(m.capacity) {
			delete(m.buckets, k)
		}
	}
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
