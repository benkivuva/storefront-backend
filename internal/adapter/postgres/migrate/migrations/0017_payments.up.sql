CREATE TABLE payments (
  id UUID PRIMARY KEY,
  order_id UUID NOT NULL REFERENCES orders(id),
  provider TEXT NOT NULL CHECK (provider IN ('paystack','mpesa_daraja','mock')),
  method TEXT NOT NULL CHECK (method IN ('card','airtel_money','mpesa')),
  status TEXT NOT NULL CHECK (status IN ('INITIATED','PENDING','SUCCEEDED','FAILED','EXPIRED','ORPHANED','REFUNDED')),
  amount_minor BIGINT NOT NULL, 
  currency CHAR(3) NOT NULL DEFAULT 'KES',
  provider_ref TEXT,
  provider_txn_id TEXT,
  failure_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(), 
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider, provider_ref)
);
CREATE INDEX payments_order_idx ON payments (order_id);
CREATE UNIQUE INDEX one_open_payment_per_order ON payments (order_id) WHERE status IN ('INITIATED','PENDING');
