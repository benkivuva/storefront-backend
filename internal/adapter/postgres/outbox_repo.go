package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type OutboxRepo struct {
	pool *pgxpool.Pool
}

func NewOutboxRepo(pool *pgxpool.Pool) *OutboxRepo {
	return &OutboxRepo{pool: pool}
}

func (r *OutboxRepo) Create(ctx context.Context, e domain.OutboxEvent) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, payload, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
		toUUID(e.ID), e.AggregateType, toUUID(e.AggregateID), e.EventType, e.Payload, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("outbox create: %w", err)
	}
	return nil
}
