CREATE INDEX products_created_at_idx ON products (created_at DESC) WHERE deleted_at IS NULL;
