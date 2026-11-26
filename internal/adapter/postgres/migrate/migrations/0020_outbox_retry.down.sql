DROP INDEX IF EXISTS outbox_pending_idx;
CREATE INDEX outbox_pending_idx ON outbox_events (id) WHERE status = 'pending';

ALTER TABLE outbox_events DROP COLUMN next_attempt_at;
