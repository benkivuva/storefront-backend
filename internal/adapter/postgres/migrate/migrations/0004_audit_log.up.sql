CREATE TABLE audit_log (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor UUID NOT NULL,
  action TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  before JSONB,
  after JSONB,
  ip INET,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_actor_idx ON audit_log (actor, created_at DESC);
CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id, created_at DESC);
