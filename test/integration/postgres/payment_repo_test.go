package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func TestPaymentRepo_Lifecycle(t *testing.T) {
	pool, _ := setupOutboxDB(t) // Reuses the DB container setup
	ctx := context.Background()
	repo := postgres.NewPaymentRepo(pool)
	gen := idgen.New()

	// 1. Setup dependencies (User & Order) to satisfy foreign keys
	userID := gen.NewID()
	_, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role, is_active, created_at, updated_at) VALUES ($1, 'pay-test@test.com', 'hash', 'customer', true, now(), now())`, userID.String())
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	orderID := gen.NewID()
	_, err = pool.Exec(ctx, `INSERT INTO orders (id, user_id, status, subtotal_minor, delivery_fee_minor, tax_minor, total_minor, currency, payment_expires_at, idempotency_key, created_at, updated_at) VALUES ($1, $2, 'PENDING_PAYMENT', 10000, 0, 0, 10000, 'KES', now(), 'idem-1', now(), now())`, orderID.String(), userID.String())
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}

	// 2. Create a payment
	paymentID := gen.NewID()
	now := time.Now().UTC().Truncate(time.Millisecond)

	p, err := domain.NewPayment(paymentID, orderID, "mock", domain.MethodCard, 10000, "KES", now)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}

	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 3. GetByID
	got, err := repo.GetByID(ctx, paymentID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status() != domain.PaymentInitiated {
		t.Errorf("expected INITIATED, got %s", got.Status())
	}
	if got.AmountMinor() != 10000 {
		t.Errorf("expected amount 10000, got %d", got.AmountMinor())
	}

	// 4. Mark Pending and Update
	err = got.MarkPending("prov-ref-123", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := repo.UpdateStatus(ctx, got); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	// 5. GetByProviderRef (crucial for webhooks)
	got2, err := repo.GetByProviderRef(ctx, "mock", "prov-ref-123")
	if err != nil {
		t.Fatalf("GetByProviderRef: %v", err)
	}
	if got2.ID() != paymentID {
		t.Errorf("expected payment ID %s, got %s", paymentID, got2.ID())
	}
	if got2.Status() != domain.PaymentPending {
		t.Errorf("expected PENDING, got %s", got2.Status())
	}

	// 6. Mark Succeeded and Update
	err = got2.MarkSucceeded("txn-999", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	if err := repo.UpdateStatus(ctx, got2); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	final, _ := repo.GetByID(ctx, paymentID)
	if final.Status() != domain.PaymentSucceeded {
		t.Errorf("expected SUCCEEDED, got %s", final.Status())
	}
	if final.ProviderTxnID() != "txn-999" {
		t.Errorf("expected txn-999, got %s", final.ProviderTxnID())
	}
}
