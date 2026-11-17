package mailer

import (
	"context"
	"log/slog"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type LogMailer struct{}

func NewLogMailer() *LogMailer {
	return &LogMailer{}
}

func (m *LogMailer) Send(ctx context.Context, msg domain.MailMessage) error {
	slog.InfoContext(ctx, "email sent (mock)",
		"to", msg.To,
		"subject", msg.Subject,
		"body", msg.Body,
	)
	return nil
}
