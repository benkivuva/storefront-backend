package mailer_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/benkivuva/storefront-backend/internal/adapter/mailer"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

func TestSMTPMailer_Send(t *testing.T) {
	ctx := context.Background()

	// 1. Spin up Mailpit (a lightweight SMTP test server)
	req := testcontainers.ContainerRequest{
		Image:        "axllent/mailpit:latest",
		ExposedPorts: []string{"1025/tcp", "8025/tcp"}, // 1025 for SMTP, 8025 for HTTP API
		WaitingFor:   wait.ForListeningPort("1025/tcp"),
	}
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start mailpit: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	// 2. Get the dynamic port
	port, err := ctr.MappedPort(ctx, "1025")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	host, _ := ctr.Host(ctx)
	smtpAddr := host + ":" + port.Port()

	// 3. Create the adapter
	sender := mailer.NewSMTPMailer(smtpAddr, "no-reply@storefront.co.ke")

	// 4. Send an email
	msg := domain.MailMessage{
		To:      "customer@example.com",
		Subject: "Order Confirmation",
		Body:    "Thank you for your order!",
	}

	// Add a small delay to let Mailpit fully boot its SMTP listener
	time.Sleep(500 * time.Millisecond)

	if err := sender.Send(ctx, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
}
