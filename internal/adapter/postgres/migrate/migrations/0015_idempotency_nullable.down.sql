-- Down migration is not strictly possible without data loss, but for testing:
ALTER TABLE idempotency_keys ALTER COLUMN response_status SET NOT NULL;
ALTER TABLE idempotency_keys ALTER COLUMN response_body SET NOT NULL;
