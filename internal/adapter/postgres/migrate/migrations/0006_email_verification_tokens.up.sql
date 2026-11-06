CREATE TABLE email_verification_tokens (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  token_hash BYTEA NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX email_verification_tokens_user_idx
  ON email_verification_tokens (user_id) WHERE used_at IS NULL;
