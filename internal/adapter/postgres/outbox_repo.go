package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, payload, trace_context, created_at)
VALUES ($1, $2, $3, $4, $5, nullif($6, '')::jsonb, $7)`,
		toUUID(e.ID), e.AggregateType, toUUID(e.AggregateID), e.EventType, e.Payload, e.TraceContext, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("outbox create: %w", err)
	}
	return nil
}

func (r *OutboxRepo) FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	rows, err := r.pool.Query(ctx, `
SELECT event_id, aggregate_type, aggregate_id, event_type, payload, attempts, coalesce(last_error, ''), created_at, coalesce(trace_context::text, '')
FROM outbox_events
WHERE status = 'pending'
ORDER BY id ASC
LIMIT $1
FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch pending outbox: %w", err)
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		if err := rows.Scan(
			(*[16]byte)(&e.ID),
			&e.AggregateType,
			(*[16]byte)(&e.AggregateID),
			&e.EventType,
			&e.Payload,
			&e.Attempts,
			&e.LastError,
			&e.CreatedAt,
			&e.TraceContext,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox events: %w", err)
	}
	return events, nil
}

func (r *OutboxRepo) FetchPublishedInRange(ctx context.Context, from, to time.Time, limit int) ([]domain.OutboxEvent, error) {
	rows, err := r.pool.Query(ctx, `
SELECT event_id, aggregate_type, aggregate_id, event_type, payload, attempts, coalesce(last_error, ''), created_at, coalesce(trace_context::text, '')
FROM outbox_events
WHERE status = 'published' AND created_at >= $1 AND created_at <= $2
ORDER BY id ASC
LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch published outbox: %w", err)
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		if err := rows.Scan(
			(*[16]byte)(&e.ID),
			&e.AggregateType,
			(*[16]byte)(&e.AggregateID),
			&e.EventType,
			&e.Payload,
			&e.Attempts,
			&e.LastError,
			&e.CreatedAt,
			&e.TraceContext,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox events: %w", err)
	}
	return events, nil
}

func (r *OutboxRepo) MarkPublished(ctx context.Context, id domain.ID) error {
	_, err := r.pool.Exec(ctx, `
UPDATE outbox_events 
SET status = 'published', published_at = now() 
WHERE event_id = $1`, toUUID(id))
	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	return nil
}

func (r *OutboxRepo) MarkDead(ctx context.Context, id domain.ID, reason string) error {
	_, err := r.pool.Exec(ctx, `
UPDATE outbox_events 
SET status = 'dead', attempts = attempts + 1, last_error = $2 
WHERE event_id = $1`, toUUID(id), reason)
	if err != nil {
		return fmt.Errorf("mark outbox dead: %w", err)
	}
	return nil
}

func (r *OutboxRepo) DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
DELETE FROM outbox_events 
WHERE status = 'published' AND created_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete published outbox events: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *OutboxRepo) OldestPending(ctx context.Context) (time.Time, error) {
	var oldest time.Time
	err := r.pool.QueryRow(ctx, `
SELECT created_at FROM outbox_events 
WHERE status = 'pending' 
ORDER BY created_at ASC 
LIMIT 1`).Scan(&oldest)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("oldest pending outbox: %w", err)
	}
	return oldest, nil
}
