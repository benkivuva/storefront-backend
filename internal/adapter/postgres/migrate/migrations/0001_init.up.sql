-- Extensions must exist before any table that uses them.
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Identity (authentication tables land in 0002.)
CREATE TABLE users (
  id UUID PRIMARY KEY,
  email CITEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'customer' CHECK (role IN ('customer','admin')),
  phone_e164 TEXT,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Catalog.
CREATE TABLE categories (
  id UUID PRIMARY KEY,
  parent_id UUID REFERENCES categories(id),
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL
);

CREATE TABLE tax_rates (
  tax_class TEXT NOT NULL CHECK (tax_class IN ('STANDARD_16','ZERO_RATED','EXEMPT')),
  rate_bp INT NOT NULL CHECK (rate_bp >= 0),
  effective_from DATE NOT NULL,
  PRIMARY KEY (tax_class, effective_from)
);

CREATE TABLE products (
  id UUID PRIMARY KEY,
  sku TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT,
  category_id UUID REFERENCES categories(id),
  price_minor BIGINT NOT NULL CHECK (price_minor >= 0),
  currency CHAR(3) NOT NULL DEFAULT 'KES',
  tax_class TEXT NOT NULL CHECK (tax_class IN ('STANDARD_16','ZERO_RATED','EXEMPT')),
  etims_item_code TEXT,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  version INT NOT NULL DEFAULT 1,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX products_category_idx ON products (category_id) WHERE deleted_at IS NULL;
CREATE INDEX products_search_idx ON products USING GIN (to_tsvector('simple', name || ' ' || coalesce(description,'')));
CREATE INDEX products_name_trgm ON products USING GIN (name gin_trgm_ops);

-- Inventory (single authority for stock; the movement ledger lands in 0002
-- alongside the code that writes to it.)
CREATE TABLE inventory (
  product_id UUID PRIMARY KEY REFERENCES products(id),
  available INT NOT NULL CHECK (available >= 0),
  low_stock_threshold INT NOT NULL DEFAULT 10,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX inventory_low_idx ON inventory (product_id) WHERE available <= low_stock_threshold;

-- Reference data: Kenya VAT rates as of launch. Changes require new
-- migrations (effective_from is the versioning mechanism).
INSERT INTO tax_rates (tax_class, rate_bp, effective_from) VALUES
  ('STANDARD_16', 1600, '2020-01-01'),
  ('ZERO_RATED',     0, '2020-01-01'),
  ('EXEMPT',         0, '2020-01-01');
