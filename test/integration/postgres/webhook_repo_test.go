package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func TestWebhookRepo_IsProcessed(t *testing.T) {
	pool, _ := setupOutboxDB(t)
	ctx := context.Background()
	repo := postgres.NewWebhookRepo(pool, idgen.New())

	// 1. Should return false for unseen event
	processed, err := repo.IsProcessed(ctx, "mock", "evt-never-seen")
	if err != nil {
		t.Fatalf("IsProcessed: %v", err)
	}
	if processed {
		t.Fatal("expected false for unseen event")
	}
}

func TestWebhookRepo_ProcessWebhookTx_AtomicSuccess(t *testing.T) {
	pool, _ := setupOutboxDB(t)
	ctx := context.Background()
	gen := idgen.New()
	repo := postgres.NewWebhookRepo(pool, gen)

	// Setup dependencies: User + Order + Payment
	userID := gen.NewID()
	_, _ = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role, is_active, created_at, updated_at) VALUES ($1, 'wh@test.com', 'h', 'customer', true, now(), now())`, userID.String())

	orderID := gen.NewID()
	_, _ = pool.Exec(ctx, `INSERT INTO orders (id, user_id, status, subtotal_minor, delivery_fee_minor, tax_minor, total_minor, currency, payment_expires_at, idempotency_key, created_at, updated_at) VALUES ($1, $2, 'PENDING_PAYMENT', 10000, 0, 0, 10000, 'KES', now(), 'idem-wh', now(), now())`, orderID.String(), userID.String())

	paymentID := gen.NewID()
	now := time.Now().UTC().Truncate(time.Millisecond)
	payment, _ := domain.NewPayment(paymentID, orderID, "mock", domain.MethodCard, 10000, "KES", now)
	_ = payment.MarkPending("prov-ref-wh", now)

	paymentRepo := postgres.NewPaymentRepo(pool)
	if err := paymentRepo.Create(ctx, payment); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	// Build the order aggregate to transition
	order := domain.RehydrateOrder(
		orderID, 0, userID, domain.ID{},
		nil, mustTestMoney(t, 0),
		10000, 0, 10000, domain.StatusPendingPayment,
		nil, now.Add(15*time.Minute), now, now,
	)
	_ = payment.MarkSucceeded("txn-wh-1", now)
	_ = order.Transition(domain.StatusPaid, "webhook", "confirmed", now)

	outboxID := gen.NewID()
	outbox := &domain.OutboxEvent{
		ID:            outboxID,
		AggregateType: "order",
		AggregateID:   orderID,
		EventType:     "OrderPaid",
		Payload:       []byte(`{"test":true}`),
		CreatedAt:     now,
	}

	webhookRec := domain.WebhookRecord{
		ID:              gen.NewID(),
		Provider:        "mock",
		ProviderEventID: "evt-atomic-1",
		SignatureValid:  true,
		RawBody:         []byte(`{"event":"payment.success"}`),
		ReceivedAt:      now,
	}

	// Run the atomic transaction
	if err := repo.ProcessWebhookTx(ctx, webhookRec, payment, order, outbox); err != nil {
		t.Fatalf("ProcessWebhookTx: %v", err)
	}

	// Verify ALL 4 writes succeeded in one transaction:

	// 1. Webhook inbox
	var webhookCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM payment_webhook_events WHERE provider = 'mock' AND provider_event_id = 'evt-atomic-1'`).Scan(&webhookCount)
	if webhookCount != 1 {
		t.Errorf("webhook inbox: expected 1, got %d", webhookCount)
	}

	// 2. Payment status
	updatedPayment, _ := paymentRepo.GetByID(ctx, paymentID)
	if updatedPayment.Status() != domain.PaymentSucceeded {
		t.Errorf("payment: expected SUCCEEDED, got %s", updatedPayment.Status())
	}

	// 3. Order status
	var orderStatus string
	_ = pool.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, orderID.String()).Scan(&orderStatus)
	if orderStatus != "PAID" {
		t.Errorf("order: expected PAID, got %s", orderStatus)
	}

	// 4. Outbox event
	var outboxCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_id = $1`, outboxID.String()).Scan(&outboxCount)
	if outboxCount != 1 {
		t.Errorf("outbox: expected 1, got %d", outboxCount)
	}

	// Now IsProcessed should return true
	processed, _ := repo.IsProcessed(ctx, "mock", "evt-atomic-1")
	if !processed {
		t.Error("expected IsProcessed to return true after commit")
	}
}

func TestWebhookRepo_DuplicateRejected(t *testing.T) {
	pool, _ := setupOutboxDB(t)
	ctx := context.Background()
	gen := idgen.New()
	repo := postgres.NewWebhookRepo(pool, gen)

	now := time.Now()
	webhookRec := domain.WebhookRecord{
		ID:              gen.NewID(),
		Provider:        "mock",
		ProviderEventID: "evt-dupe",
		SignatureValid:  true,
		RawBody:         []byte(`{}`),
		ReceivedAt:      now,
	}

	// First insert should succeed (no payment/order/outbox)
	if err := repo.ProcessWebhookTx(ctx, webhookRec, nil, nil, nil); err != nil {
		t.Fatalf("first ProcessWebhookTx: %v", err)
	}

	// Second insert with the SAME provider_event_id should fail with ErrConflict
	webhookRec2 := webhookRec
	webhookRec2.ID = gen.NewID() // Different DB id, same event id

	err := repo.ProcessWebhookTx(ctx, webhookRec2, nil, nil, nil)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate, got %v", err)
	}
}

func TestWebhookRepo_InvalidSignatureStillRecorded(t *testing.T) {
	pool, _ := setupOutboxDB(t)
	ctx := context.Background()
	gen := idgen.New()
	repo := postgres.NewWebhookRepo(pool, gen)

	now := time.Now()
	webhookRec := domain.WebhookRecord{
		ID:              gen.NewID(),
		Provider:        "mock",
		ProviderEventID: "evt-bad-sig",
		SignatureValid:  false,
		RawBody:         []byte(`{"tampered":true}`),
		ReceivedAt:      now,
	}

	if err := repo.ProcessWebhookTx(ctx, webhookRec, nil, nil, nil); err != nil {
		t.Fatalf("ProcessWebhookTx: %v", err)
	}

	var sigValid bool
	_ = pool.QueryRow(ctx, `SELECT signature_valid FROM payment_webhook_events WHERE provider_event_id = 'evt-bad-sig'`).Scan(&sigValid)
	if sigValid {
		t.Error("expected signature_valid to be false")
	}
}

func mustTestMoney(t *testing.T, m int64) domain.Money {
	t.Helper()
	money, err := domain.New(m, domain.KES)
	if err != nil {
		t.Fatal(err)
	}
	return money
}
