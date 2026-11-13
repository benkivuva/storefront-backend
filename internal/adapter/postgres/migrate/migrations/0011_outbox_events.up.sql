CREATE TABLE outbox_events (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_id UUID NOT NULL UNIQUE,
  aggregate_type TEXT NOT NULL,
  aggregate_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  schema_version INT NOT NULL DEFAULT 1,
  payload JSONB NOT NULL,
  trace_context JSONB,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','published','dead')),
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);

CREATE INDEX outbox_pending_idx ON outbox_events (id) WHERE status = 'pending';
