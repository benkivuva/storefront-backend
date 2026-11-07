package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var ErrInvalidCapacity = errors.New("ratelimit: capacity must be positive")
var ErrInvalidWindow = errors.New("ratelimit: window must be positive")

// Decision is the outcome of a rate-limit check.
type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// Limiter is a Redis-backed token bucket. Each Allow call reads the bucket
// state, refills by elapsed time, tries to consume cost tokens, and writes
// the updated state — all in one Lua script so concurrent calls cannot
// both succeed against the same token.
type Limiter struct {
	client       goredis.UniversalClient
	capacity     int
	refillPerSec float64
	script       *goredis.Script
}

// NewLimiter constructs a rate limiter that allows `capacity` requests per
// `window`. A caller wanting different limits for different endpoints
// constructs multiple Limiter instances sharing the same Redis client.
func NewLimiter(client goredis.UniversalClient, capacity int, window time.Duration) (*Limiter, error) {
	if capacity <= 0 {
		return nil, ErrInvalidCapacity
	}
	if window <= 0 {
		return nil, ErrInvalidWindow
	}
	return &Limiter{
		client:       client,
		capacity:     capacity,
		refillPerSec: float64(capacity) / window.Seconds(),
		script:       goredis.NewScript(tokenBucketLua),
	}, nil
}

// Allow consumes cost tokens from the bucket identified by key.
func (l *Limiter) Allow(ctx context.Context, key string, cost int) (Decision, error) {
	if cost < 1 {
		cost = 1
	}
	if cost > l.capacity {
		return Decision{Allowed: false, Remaining: 0}, nil
	}
	now := float64(time.Now().UnixNano()) / 1e9
	raw, err := l.script.Run(ctx, l.client, []string{key},
		l.capacity, l.refillPerSec, now, cost).Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit eval: %w", err)
	}
	if len(raw) != 3 {
		return Decision{}, fmt.Errorf("ratelimit: unexpected script reply length %d", len(raw))
	}
	allowed, _ := raw[0].(int64)
	remaining, _ := raw[1].(int64)
	retrySecs, _ := raw[2].(int64)
	return Decision{
		Allowed:    allowed == 1,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retrySecs) * time.Second,
	}, nil
}

// tokenBucketLua implements a token bucket. KEYS[1] is the bucket key;
// ARGV is capacity, refill-per-second, now (unix seconds, float), cost.
// Returns {allowed, tokens_remaining, retry_after_seconds}.
const tokenBucketLua = `
local key           = KEYS[1]
local capacity      = tonumber(ARGV[1])
local refill_per_s  = tonumber(ARGV[2])
local now           = tonumber(ARGV[3])
local cost          = tonumber(ARGV[4])

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens  = tonumber(data[1])
local last_ts = tonumber(data[2])

if tokens == nil then
  tokens  = capacity
  last_ts = now
end

local elapsed = now - last_ts
if elapsed < 0 then elapsed = 0 end

tokens = math.min(capacity, tokens + elapsed * refill_per_s)

local allowed    = 0
local retry_after = 0

if tokens >= cost then
  tokens  = tokens - cost
  allowed = 1
else
  local needed = cost - tokens
  retry_after = math.ceil(needed / refill_per_s)
end

redis.call('HSET', key, 'tokens', tokens, 'ts', now)
local ttl = math.ceil(capacity / refill_per_s) + 60
redis.call('EXPIRE', key, ttl)

return {allowed, math.floor(tokens), retry_after}
`
