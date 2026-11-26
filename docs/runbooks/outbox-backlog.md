# Outbox backlog

## Symptom

`OutboxBacklog` or `OutboxBacklogCritical` fires because
`outbox_oldest_pending_seconds` is above 60 s / 300 s.

## Triage

1. Check the relay worker is running: `kubectl get pods -l app=storefront-worker`.
2. Check relay logs for `failed to publish outbox event` and the Redis error.
3. Check Redis streams reachability: `redis-cli -u "$REDIS_STREAMS_URL" PING`.
4. Inspect pending rows:
   ```sql
   SELECT event_id, event_type, attempts, last_error, next_attempt_at
   FROM outbox_events
   WHERE status = 'pending'
   ORDER BY id
   LIMIT 20;
   ```

## Mitigation

- Transient Redis outage: the relay retries with exponential backoff (max 5
  attempts) and marks the event `dead` only after exhaustion. Restore Redis and
  wait for the next tick.
- Poison event: inspect the payload, then replay it with
  `worker replay --from=<RFC3339> --to=<RFC3339>` or move it to the DLQ.
- Stuck relay: restart the worker; `FOR UPDATE SKIP LOCKED` makes this safe.

## Verify

`outbox_oldest_pending_seconds` returns below 10 s and no new `dead` rows appear.
