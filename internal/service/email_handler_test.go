package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

type mockMailer struct {
	sent []domain.MailMessage
	fail bool
}

func (m *mockMailer) Send(ctx context.Context, msg domain.MailMessage) error {
	if m.fail {
		return errors.New("smtp down")
	}
	m.sent = append(m.sent, msg)
	return nil
}

type mockUserRepo struct {
	user domain.User
}

func (m *mockUserRepo) Create(ctx context.Context, u domain.User) error { return nil }
func (m *mockUserRepo) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	return m.user, nil
}
func (m *mockUserRepo) FindByID(ctx context.Context, id domain.ID) (domain.User, error) {
	return m.user, nil
}
func (m *mockUserRepo) UpdateProfile(ctx context.Context, id domain.ID, name, phone *string, at time.Time) error {
	return nil
}
func (m *mockUserRepo) UpdatePasswordHash(ctx context.Context, id domain.ID, hash string, at time.Time) error {
	return nil
}

func TestEmailHandler_OrderCreated_SendsReceipt(t *testing.T) {
	mailer := &mockMailer{}

	userID := domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	user, _ := domain.NewUser(userID, "customer@test.local", "hash", "Cust", domain.RoleCustomer, "", time.Now())
	users := &mockUserRepo{user: user}

	handler := service.NewEmailHandler(mailer, users)

	payload := map[string]any{
		"id":          "order-123",
		"user_id":     userID.String(),
		"total_minor": 70000,
		"currency":    "KES",
	}
	payloadBytes, _ := json.Marshal(payload)

	evt := domain.OutboxEvent{
		ID:        id1,
		EventType: "OrderCreated",
		Payload:   payloadBytes,
	}

	if err := handler.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(mailer.sent) != 1 {
		t.Fatalf("expected 1 email, got %d", len(mailer.sent))
	}
	msg := mailer.sent[0]
	if msg.To != "customer@test.local" {
		t.Errorf("expected customer@test.local, got %s", msg.To)
	}
	if msg.Subject == "" {
		t.Error("expected non-empty subject")
	}
}

func TestEmailHandler_IgnoresOtherEvents(t *testing.T) {
	mailer := &mockMailer{}
	users := &mockUserRepo{}
	handler := service.NewEmailHandler(mailer, users)

	evt := domain.OutboxEvent{
		ID:        id1,
		EventType: "OrderPaid",
		Payload:   []byte(`{}`),
	}

	if err := handler.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(mailer.sent) != 0 {
		t.Errorf("expected 0 emails for unrelated event, got %d", len(mailer.sent))
	}
}

func TestEmailHandler_MailerFails_ReturnsError(t *testing.T) {
	mailer := &mockMailer{fail: true}

	userID := domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	user, _ := domain.NewUser(userID, "customer@test.local", "hash", "Cust", domain.RoleCustomer, "", time.Now())
	users := &mockUserRepo{user: user}

	handler := service.NewEmailHandler(mailer, users)

	payload := map[string]any{
		"id":          "order-123",
		"user_id":     userID.String(),
		"total_minor": 70000,
		"currency":    "KES",
	}
	payloadBytes, _ := json.Marshal(payload)

	evt := domain.OutboxEvent{
		ID:        id1,
		EventType: "OrderCreated",
		Payload:   payloadBytes,
	}

	if err := handler.Handle(context.Background(), evt); err == nil {
		t.Fatal("expected error when mailer fails")
	}
}
