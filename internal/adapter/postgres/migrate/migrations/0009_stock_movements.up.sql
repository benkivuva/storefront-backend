CREATE TABLE stock_movements (
  id UUID PRIMARY KEY,
  product_id UUID NOT NULL REFERENCES products(id),
  delta INT NOT NULL,
  reason TEXT NOT NULL CHECK (reason IN (
    'ORDER_RESERVE','ORDER_RELEASE','RESTOCK','ADJUSTMENT','SEED'
  )),
  order_id UUID,
  actor UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX stock_movements_product_idx ON stock_movements (product_id, created_at DESC);
