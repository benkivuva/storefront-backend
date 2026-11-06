CREATE TABLE refresh_tokens (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  family_id UUID NOT NULL,
  token_hash BYTEA NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  replaced_by UUID,
  user_agent TEXT,
  ip INET,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);

-- Supports "revoke every active token in this family":
-- UPDATE refresh_tokens SET revoked_at = now()
-- WHERE family_id = $1 AND revoked_at IS NULL
CREATE INDEX refresh_tokens_family_active_idx
  ON refresh_tokens (family_id) WHERE revoked_at IS NULL;
