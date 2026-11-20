CREATE TABLE payment_webhook_events (
  id UUID PRIMARY KEY, 
  provider TEXT NOT NULL, 
  provider_event_id TEXT NOT NULL,
  signature_valid BOOLEAN NOT NULL, 
  raw_body JSONB NOT NULL, 
  source_ip INET,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(), 
  processed_at TIMESTAMPTZ,
  UNIQUE (provider, provider_event_id)
);
