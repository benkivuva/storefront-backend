package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type IdempotencyRepo struct {
	pool *pgxpool.Pool
}

func NewIdempotencyRepo(pool *pgxpool.Pool) *IdempotencyRepo {
	return &IdempotencyRepo{pool: pool}
}

func (r *IdempotencyRepo) Get(ctx context.Context, userID domain.ID, key string) (domain.IdempotencyRecord, error) {
	var status int
	var rawBody []byte

	err := r.pool.QueryRow(ctx, `
SELECT response_status, response_body FROM idempotency_keys
WHERE user_id = $1 AND key = $2`,
		toUUID(userID), key).Scan(&status, &rawBody)

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IdempotencyRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.IdempotencyRecord{}, fmt.Errorf("idempotency get: %w", err)
	}

	return domain.IdempotencyRecord{Status: status, Body: rawBody}, nil
}

func (r *IdempotencyRepo) Save(ctx context.Context, userID domain.ID, key string, requestHash []byte, status int, body []byte) error {
	var rawBody json.RawMessage = body

	// ON CONFLICT DO UPDATE guarantees atomicity. If the key exists but the
	// hash doesn't match, the WHERE clause fails and 0 rows are returned.
	tag, err := r.pool.Exec(ctx, `
INSERT INTO idempotency_keys (user_id, key, request_hash, response_status, response_body)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, key) DO UPDATE
SET response_status = EXCLUDED.response_status,
    response_body   = EXCLUDED.response_body
WHERE idempotency_keys.request_hash = EXCLUDED.request_hash`,
		toUUID(userID), key, requestHash, status, rawBody)

	if err != nil {
		return fmt.Errorf("idempotency save: %w", err)
	}

	if tag.RowsAffected() == 0 {
		// Means the row existed but request_hash didn't match (or the insert failed due to some other constraint).
		// Because of the ON CONFLICT, we know it's the hash mismatch path.
		return domain.ErrConflict
	}

	return nil
}
