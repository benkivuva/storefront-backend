CREATE TABLE orders (
  id UUID PRIMARY KEY,
  order_number BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
  user_id UUID NOT NULL REFERENCES users(id),
  shipping_address_id UUID REFERENCES addresses(id),
  status TEXT NOT NULL CHECK (status IN ('PENDING_PAYMENT','PAID','FULFILLING','SHIPPED','DELIVERED','CANCELLED','REFUND_PENDING','REFUNDED')),
  subtotal_minor BIGINT NOT NULL,
  delivery_fee_minor BIGINT NOT NULL DEFAULT 0,
  tax_minor BIGINT NOT NULL,
  total_minor BIGINT NOT NULL,
  currency CHAR(3) NOT NULL DEFAULT 'KES',
  payment_expires_at TIMESTAMPTZ NOT NULL,
  idempotency_key TEXT NOT NULL,
  etims_status TEXT NOT NULL DEFAULT 'NOT_REQUIRED',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, idempotency_key)
);

CREATE INDEX orders_user_created_idx ON orders (user_id, created_at DESC);
CREATE INDEX orders_reaper_idx ON orders (payment_expires_at) WHERE status = 'PENDING_PAYMENT';

CREATE TABLE order_items (
  id UUID PRIMARY KEY,
  order_id UUID NOT NULL REFERENCES orders(id),
  product_id UUID NOT NULL REFERENCES products(id),
  sku TEXT NOT NULL,
  product_name TEXT NOT NULL,
  quantity INT NOT NULL CHECK (quantity > 0),
  unit_price_minor BIGINT NOT NULL,
  tax_class TEXT NOT NULL,
  tax_rate_bp INT NOT NULL,
  line_gross_minor BIGINT NOT NULL,
  line_vat_minor BIGINT NOT NULL
);
CREATE INDEX order_items_order_idx ON order_items (order_id);

CREATE TABLE order_status_history (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  order_id UUID NOT NULL REFERENCES orders(id),
  from_status TEXT,
  to_status TEXT NOT NULL,
  actor TEXT NOT NULL,
  reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_history_order_idx ON order_status_history (order_id);
