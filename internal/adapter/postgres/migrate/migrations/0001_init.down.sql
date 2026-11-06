-- Reverse order of 0001_init.up.sql. Foreign-key chain: inventory -> products
-- -> categories. tax_rates and users are independent.
--
-- Extensions (citext, pg_trgm) are intentionally left in place. Dropping an
-- extension that another schema, another app, or another migration depends on
-- will cascade-fail or, worse, take down objects we don't own. Leaving them
-- installed costs nothing; creating them is idempotent (IF NOT EXISTS).

DROP TABLE IF EXISTS inventory;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS tax_rates;
DROP TABLE IF EXISTS users;
