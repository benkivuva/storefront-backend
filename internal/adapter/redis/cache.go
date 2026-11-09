package redis

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type Cache struct {
	client goredis.UniversalClient
	group  singleflight.Group
	prefix string
}

func NewCache(client goredis.UniversalClient, prefix string) *Cache {
	return &Cache{
		client: client,
		prefix: prefix,
	}
}

// GetOrFetch attempts to get the value from Redis. If it misses, it uses
// singleflight to ensure only one concurrent caller executes fetchFn.
// If fetchFn returns ErrNotFound, a negative "null" cache is set for 5s.
func (c *Cache) GetOrFetch(ctx context.Context, key string, ttl time.Duration, fetchFn func() ([]byte, error)) ([]byte, error) {
	fullKey := c.prefix + ":" + key

	// 1. Try Redis
	val, err := c.client.Get(ctx, fullKey).Bytes()
	if err == nil {
		if string(val) == "null" {
			return nil, domain.ErrNotFound
		}
		return val, nil
	}
	if !errors.Is(err, goredis.Nil) {
		// Redis is down. Degrade gracefully by hitting the DB directly.
		return fetchFn()
	}

	// 2. Cache Miss -> Singleflight
	res, err, _ := c.group.Do(fullKey, func() (any, error) {
		// Double-check: another goroutine might have populated it while we waited.
		val, err := c.client.Get(ctx, fullKey).Bytes()
		if err == nil {
			if string(val) == "null" {
				return nil, domain.ErrNotFound
			}
			return val, nil
		}

		// We are the leader. Fetch from DB.
		b, err := fetchFn()
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				// Negative caching: prevent hammering DB for bad IDs.
				_ = c.client.Set(ctx, fullKey, []byte("null"), 5*time.Second).Err()
			}
			return nil, err
		}

		// Add jitter: +/- 10% so a whole batch of keys doesn't expire at the exact same second.
		// #nosec G404 -- non-cryptographic randomness is sufficient for cache jitter.
		jitter := time.Duration(rand.Int64N(int64(ttl)/5)) - ttl/10
		_ = c.client.Set(ctx, fullKey, b, ttl+jitter).Err()

		return b, nil
	})

	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

// Delete removes keys from the cache (used on admin writes).
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	fullKeys := make([]string, len(keys))
	for i, k := range keys {
		fullKeys[i] = c.prefix + ":" + k
	}
	return c.client.Del(ctx, fullKeys...).Err()
}
