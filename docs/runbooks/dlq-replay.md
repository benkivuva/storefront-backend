# DLQ and pending-entry recovery

## Symptom

`DeadLetterQueueNotEmpty` or `StreamPendingHigh` fires.

## Triage

1. Inspect the DLQ:
   ```bash
   redis-cli -u "$REDIS_STREAMS_URL" XRANGE events:dlq - + COUNT 20
   ```
2. Inspect pending entries for the email consumer group:
   ```bash
   redis-cli -u "$REDIS_STREAMS_URL" XPENDING events:order email-group - + 20
   ```
3. Correlate `event_id` with `outbox_events` and `processed_events`.

## Recovery

- The consumer automatically calls `XAUTOCLAIM` every 30 s for entries idle
  longer than 30 s, so a crashed consumer's work is picked up by a live one.
- A handler failure is written to `events:dlq` and acked. Replay by re-adding
  the payload to `events:order` after fixing the handler, or by running
  `worker replay` for the original time window. Consumers dedupe through
  `processed_events`, so replays are safe.
- Never delete `processed_events` rows to force a replay unless you intend to
  duplicate side effects.
