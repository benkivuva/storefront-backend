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
	var status *int
	var rawBody []byte
	var hash []byte

	err := r.pool.QueryRow(ctx, `
SELECT response_status, response_body, request_hash FROM idempotency_keys
WHERE user_id = $1 AND key = $2`,
		toUUID(userID), key).Scan(&status, &rawBody, &hash)

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IdempotencyRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.IdempotencyRecord{}, fmt.Errorf("idempotency get: %w", err)
	}
	if status == nil {
		return domain.IdempotencyRecord{Hash: hash}, domain.ErrConflict // In progress
	}

	return domain.IdempotencyRecord{Status: *status, Body: rawBody, Hash: hash}, nil
}

func (r *IdempotencyRepo) Lock(ctx context.Context, userID domain.ID, key string, requestHash []byte) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO idempotency_keys (user_id, key, request_hash)
VALUES ($1, $2, $3)`,
		toUUID(userID), key, requestHash)

	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("idempotency lock: %w", err)
	}
	return nil
}

func (r *IdempotencyRepo) Save(ctx context.Context, userID domain.ID, key string, status int, body []byte) error {
	var rawBody json.RawMessage = body
	_, err := r.pool.Exec(ctx, `
UPDATE idempotency_keys
SET response_status = $3, response_body = $4
WHERE user_id = $1 AND key = $2`,
		toUUID(userID), key, status, rawBody)

	if err != nil {
		return fmt.Errorf("idempotency save: %w", err)
	}
	return nil
}

func (r *IdempotencyRepo) Delete(ctx context.Context, userID domain.ID, key string) error {
	_, err := r.pool.Exec(ctx, `
DELETE FROM idempotency_keys
WHERE user_id = $1 AND key = $2`,
		toUUID(userID), key)

	if err != nil {
		return fmt.Errorf("idempotency delete: %w", err)
	}
	return nil
}
