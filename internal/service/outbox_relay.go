package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

const maxPublishAttempts = 5

type OutboxRelay struct {
	repo domain.OutboxRepository
	pub  domain.EventPublisher
	now  func() time.Time
}

func NewOutboxRelay(repo domain.OutboxRepository, pub domain.EventPublisher) *OutboxRelay {
	return &OutboxRelay{
		repo: repo,
		pub:  pub,
		now:  time.Now,
	}
}

// ProcessBatch fetches pending events, publishes them, and marks them
// published. A failed publish is retried with exponential backoff; only after
// maxPublishAttempts is the event dead-lettered so a transient broker outage
// cannot permanently drop an event.
func (r *OutboxRelay) ProcessBatch(ctx context.Context, limit int) error {
	events, err := r.repo.FetchPending(ctx, limit)
	if err != nil {
		return fmt.Errorf("fetch pending events: %w", err)
	}

	for _, event := range events {
		if err := r.pub.Publish(ctx, event); err != nil {
			slog.ErrorContext(ctx, "failed to publish outbox event",
				"event_id", event.ID, "attempts", event.Attempts+1, "error", err)

			if event.Attempts+1 >= maxPublishAttempts {
				if deadErr := r.repo.MarkDead(ctx, event.ID, err.Error()); deadErr != nil {
					slog.ErrorContext(ctx, "failed to mark event dead", "event_id", event.ID, "error", deadErr)
				}
				continue
			}

			backoff := time.Duration(1<<uint(event.Attempts)) * time.Second
			if backoff > time.Minute {
				backoff = time.Minute
			}
			if retryErr := r.repo.MarkRetry(ctx, event.ID, err.Error(), r.now().Add(backoff)); retryErr != nil {
				slog.ErrorContext(ctx, "failed to schedule event retry", "event_id", event.ID, "error", retryErr)
			}
			continue
		}

		if err := r.repo.MarkPublished(ctx, event.ID); err != nil {
			slog.ErrorContext(ctx, "failed to mark event published", "event_id", event.ID, "error", err)
		}
	}

	return nil
}
