package service

import (
	"context"
	"fmt"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type ReplayService struct {
	repo domain.OutboxRepository
	pub  domain.EventPublisher
}

func NewReplayService(repo domain.OutboxRepository, pub domain.EventPublisher) *ReplayService {
	return &ReplayService{
		repo: repo,
		pub:  pub,
	}
}

func (s *ReplayService) Replay(ctx context.Context, from, to time.Time) (int, error) {
	events, err := s.repo.FetchPublishedInRange(ctx, from, to, 1000)
	if err != nil {
		return 0, fmt.Errorf("fetch published events: %w", err)
	}

	count := 0
	for _, event := range events {
		if err := s.pub.Publish(ctx, event); err != nil {
			return count, fmt.Errorf("publish event %s: %w", event.ID, err)
		}
		count++
	}
	return count, nil
}
