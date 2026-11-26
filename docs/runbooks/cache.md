# Catalog cache health

## Signals

- `cache_hits_total` / `cache_misses_total` → hit ratio panel in
  `docs/dashboards/cache-hit-ratio.json`.
- `CacheHitRatioLow` fires below 80% for 10 minutes.
- `CacheDown` fires when misses are recorded but no hits are.

## Triage

1. Check Redis cache reachability (`REDIS_CACHE_URL`).
2. Confirm admin writes are invalidating `products:list:*` (they now use
   `SCAN` + `DEL`). A burst of misses after a write is expected.
3. Check for high-cardinality query strings inflating the cache keyspace.
4. If Redis is down, the adapter falls back to Postgres; expect latency to rise
   and the ratio to drop to zero until Redis returns.

## Verify

The dashboard ratio recovers above 80% and `redis_stream_length`/latency
panels return to baseline.
