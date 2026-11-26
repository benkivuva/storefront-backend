ALTER TABLE outbox_events
  ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now();

DROP INDEX IF EXISTS outbox_pending_idx;
CREATE INDEX outbox_pending_idx ON outbox_events (next_attempt_at, id) WHERE status = 'pending';
