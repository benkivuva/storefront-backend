CREATE TABLE refunds (
  id UUID PRIMARY KEY, 
  payment_id UUID NOT NULL REFERENCES payments(id),
  amount_minor BIGINT NOT NULL, 
  status TEXT NOT NULL, 
  provider_ref TEXT, 
  reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
