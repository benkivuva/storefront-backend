CREATE TABLE addresses (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  recipient TEXT NOT NULL,
  phone_e164 TEXT NOT NULL,
  line1 TEXT NOT NULL,
  line2 TEXT,
  town TEXT NOT NULL,
  county TEXT,
  notes TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX addresses_user_idx ON addresses (user_id);
