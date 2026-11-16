package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type ProcessedEventRepo struct {
	pool *pgxpool.Pool
}

func NewProcessedEventRepo(pool *pgxpool.Pool) *ProcessedEventRepo {
	return &ProcessedEventRepo{pool: pool}
}

func (r *ProcessedEventRepo) IsProcessed(ctx context.Context, consumer string, eventID domain.ID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS(
SELECT 1 FROM processed_events WHERE consumer = $1 AND event_id = $2
)`, consumer, toUUID(eventID)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check processed event: %w", err)
	}
	return exists, nil
}

func (r *ProcessedEventRepo) MarkProcessed(ctx context.Context, consumer string, eventID domain.ID) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO processed_events (consumer, event_id) 
VALUES ($1, $2) 
ON CONFLICT (consumer, event_id) DO NOTHING`, consumer, toUUID(eventID))
	if err != nil {
		return fmt.Errorf("mark event processed: %w", err)
	}
	return nil
}
