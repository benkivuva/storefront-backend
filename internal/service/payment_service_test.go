package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
	"github.com/benkivuva/storefront-backend/internal/service"
)

type mockOrderRepo struct {
	order domain.Order
	err   error
}

func (m *mockOrderRepo) Create(ctx context.Context, o *domain.Order, idem string) error { return nil }
func (m *mockOrderRepo) GetByID(ctx context.Context, id domain.ID) (*domain.Order, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &m.order, nil
}
func (m *mockOrderRepo) UpdateStatus(ctx context.Context, o *domain.Order) error { return nil }
func (m *mockOrderRepo) Cancel(ctx context.Context, o *domain.Order) error       { return nil }
func (m *mockOrderRepo) ListByUser(ctx context.Context, userID domain.ID) ([]*domain.Order, error) {
	return nil, nil
}
func (m *mockOrderRepo) ListExpiredPending(ctx context.Context, before time.Time, limit int) ([]*domain.Order, error) {
	return nil, nil
}

type mockPaymentRepo struct {
	created   *domain.Payment
	updated   *domain.Payment
	byRef     *domain.Payment
	createErr error
}

func (m *mockPaymentRepo) Create(ctx context.Context, p *domain.Payment) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.created = p
	return nil
}
func (m *mockPaymentRepo) GetByID(ctx context.Context, id domain.ID) (*domain.Payment, error) {
	return nil, nil
}
func (m *mockPaymentRepo) GetByProviderRef(ctx context.Context, provider, ref string) (*domain.Payment, error) {
	if m.byRef != nil {
		return m.byRef, nil
	}
	return nil, domain.ErrNotFound
}
func (m *mockPaymentRepo) GetLatestByOrderID(ctx context.Context, orderID domain.ID) (*domain.Payment, error) {
	return nil, nil
}
func (m *mockPaymentRepo) ListByStatus(ctx context.Context, status domain.PaymentStatus, limit int) ([]*domain.Payment, error) {
	return nil, nil
}

func (m *mockPaymentRepo) UpdateStatus(ctx context.Context, p *domain.Payment) error {
	m.updated = p
	return nil
}

type mockProvider struct {
	initiateResult domain.InitiateResult
	initiateErr    error
	verifyResult   domain.WebhookEvent
	verifyErr      error
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Initiate(ctx context.Context, req domain.InitiateRequest) (domain.InitiateResult, error) {
	return m.initiateResult, m.initiateErr
}
func (m *mockProvider) VerifyWebhook(ctx context.Context, rawBody []byte, headers map[string]string) (domain.WebhookEvent, error) {
	return m.verifyResult, m.verifyErr
}
func (m *mockProvider) Status(ctx context.Context, ref string) (domain.WebhookEvent, error) {
	return domain.WebhookEvent{}, nil
}
func (m *mockProvider) Refund(ctx context.Context, req domain.RefundRequest) (domain.RefundResult, error) {
	return domain.RefundResult{}, nil
}

type mockWebhookRepo struct {
	processed    map[string]bool
	savedWebhook domain.WebhookRecord
	savedPayment *domain.Payment
	savedOrder   *domain.Order
	savedOutbox  *domain.OutboxEvent
}

func (m *mockWebhookRepo) IsProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	return m.processed[eventID], nil
}
func (m *mockWebhookRepo) ProcessWebhookTx(ctx context.Context, webhook domain.WebhookRecord, payment *domain.Payment, order *domain.Order, outbox *domain.OutboxEvent) error {
	m.savedWebhook = webhook
	m.savedPayment = payment
	m.savedOrder = order
	m.savedOutbox = outbox
	return nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

func makeTestOrder(t *testing.T, expiresIn time.Duration) *domain.Order {
	t.Helper()
	userID := domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	addressID := domain.ID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}
	productID := domain.ID{0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30}
	price, _ := domain.New(10000, domain.KES)
	fee, _ := domain.New(0, domain.KES)
	snapshot := domain.OrderItemSnapshot{
		ProductID: productID, SKU: "SKU", ProductName: "Item", Quantity: 1,
		UnitPrice: price, TaxClass: domain.TaxStandard16, TaxRateBp: 1600,
		LineGrossMinor: 10000, LineVat: mustMoney(t, 1379),
	}
	now := time.Now()
	order, err := domain.NewOrder(domain.ID{0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40}, userID, addressID, []domain.OrderItemSnapshot{snapshot}, fee, now, now.Add(expiresIn))
	if err != nil {
		t.Fatal(err)
	}
	return order
}

func mustMoney(t *testing.T, m int64) domain.Money {
	t.Helper()
	money, err := domain.New(m, domain.KES)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func TestInitiatePayment_Success(t *testing.T) {
	order := makeTestOrder(t, 15*time.Minute)
	orderRepo := &mockOrderRepo{order: *order}
	paymentRepo := &mockPaymentRepo{}
	prov := &mockProvider{
		initiateResult: domain.InitiateResult{
			ProviderRef: "mock-ref-123",
			NextAction:  domain.NextAction{Type: "redirect", URL: "https://pay.example.com/123"},
		},
	}
	clk := &fakeClock{t: time.Now()}

	svc := service.NewPaymentService(orderRepo, paymentRepo, prov, idgen.New(), clk)

	res, err := svc.InitiatePayment(context.Background(), order.UserID(), order.ID(), domain.MethodCard, "")
	if err != nil {
		t.Fatalf("InitiatePayment: %v", err)
	}

	if res.NextAction.Type != "redirect" {
		t.Errorf("expected redirect, got %s", res.NextAction.Type)
	}
	if paymentRepo.created == nil {
		t.Fatal("expected payment to be created")
	}
	if paymentRepo.updated == nil {
		t.Fatal("expected payment to be updated to PENDING")
	}
	if paymentRepo.updated.Status() != domain.PaymentPending {
		t.Errorf("expected PENDING, got %s", paymentRepo.updated.Status())
	}
}

func TestInitiatePayment_OrderNotFound(t *testing.T) {
	orderRepo := &mockOrderRepo{err: domain.ErrNotFound}
	paymentRepo := &mockPaymentRepo{}
	prov := &mockProvider{}
	clk := &fakeClock{t: time.Now()}

	svc := service.NewPaymentService(orderRepo, paymentRepo, prov, idgen.New(), clk)

	_, err := svc.InitiatePayment(context.Background(), domain.ID{0xff}, domain.ID{0xff}, domain.MethodCard, "")
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

func TestInitiatePayment_ProviderFails_MarksFailed(t *testing.T) {
	order := makeTestOrder(t, 15*time.Minute)
	orderRepo := &mockOrderRepo{order: *order}
	paymentRepo := &mockPaymentRepo{}
	prov := &mockProvider{initiateErr: errors.New("gateway down")}
	clk := &fakeClock{t: time.Now()}

	svc := service.NewPaymentService(orderRepo, paymentRepo, prov, idgen.New(), clk)

	_, err := svc.InitiatePayment(context.Background(), order.UserID(), order.ID(), domain.MethodCard, "")
	if err == nil {
		t.Fatal("expected error when provider fails")
	}
	if paymentRepo.updated == nil {
		t.Fatal("expected payment to be updated to FAILED")
	}
	if paymentRepo.updated.Status() != domain.PaymentFailed {
		t.Errorf("expected FAILED, got %s", paymentRepo.updated.Status())
	}
}

func TestProcessWebhook_Success(t *testing.T) {
	order := makeTestOrder(t, 15*time.Minute)
	payment, _ := domain.NewPayment(domain.ID{0x01}, order.ID(), "mock", domain.MethodCard, order.TotalMinor(), "KES", time.Now())
	_ = payment.MarkPending("ref-123", time.Now())

	orderRepo := &mockOrderRepo{order: *order}
	paymentRepo := &mockPaymentRepo{byRef: payment}
	webhookRepo := &mockWebhookRepo{processed: make(map[string]bool)}
	prov := &mockProvider{
		verifyResult: domain.WebhookEvent{
			ProviderEventID: "evt-123",
			ProviderRef:     "ref-123",
			Status:          domain.PaymentSucceeded,
			AmountMinor:     order.TotalMinor(),
			Currency:        "KES",
			SignatureValid:  true,
			RawPayload:      []byte(`{}`),
		},
	}
	clk := &fakeClock{t: time.Now()}

	svc := service.NewPaymentService(orderRepo, paymentRepo, prov, idgen.New(), clk)
	svc.SetWebhookRepo(webhookRepo)

	err := svc.ProcessWebhook(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("ProcessWebhook: %v", err)
	}

	if webhookRepo.savedPayment.Status() != domain.PaymentSucceeded {
		t.Errorf("expected SUCCEEDED, got %s", webhookRepo.savedPayment.Status())
	}
	if webhookRepo.savedOrder.Status() != domain.StatusPaid {
		t.Errorf("expected PAID, got %s", webhookRepo.savedOrder.Status())
	}
	if webhookRepo.savedOutbox.EventType != "OrderPaid" {
		t.Errorf("expected OrderPaid outbox, got %s", webhookRepo.savedOutbox.EventType)
	}
}

func TestProcessWebhook_DuplicateIgnored(t *testing.T) {
	webhookRepo := &mockWebhookRepo{processed: map[string]bool{"evt-123": true}}
	prov := &mockProvider{
		verifyResult: domain.WebhookEvent{
			ProviderEventID: "evt-123",
			SignatureValid:  true,
		},
	}
	clk := &fakeClock{t: time.Now()}

	svc := service.NewPaymentService(&mockOrderRepo{}, &mockPaymentRepo{}, prov, idgen.New(), clk)
	svc.SetWebhookRepo(webhookRepo)

	err := svc.ProcessWebhook(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("ProcessWebhook: %v", err)
	}
	if webhookRepo.savedWebhook.ID != (domain.ID{}) {
		t.Error("expected no DB writes for duplicate webhook")
	}
}

func TestProcessWebhook_AmountMismatch(t *testing.T) {
	order := makeTestOrder(t, 15*time.Minute)
	payment, _ := domain.NewPayment(domain.ID{0x01}, order.ID(), "mock", domain.MethodCard, order.TotalMinor(), "KES", time.Now())
	_ = payment.MarkPending("ref-123", time.Now())

	paymentRepo := &mockPaymentRepo{byRef: payment}
	webhookRepo := &mockWebhookRepo{processed: make(map[string]bool)}
	prov := &mockProvider{
		verifyResult: domain.WebhookEvent{
			ProviderEventID: "evt-123",
			ProviderRef:     "ref-123",
			Status:          domain.PaymentSucceeded,
			AmountMinor:     500,
			Currency:        "KES",
			SignatureValid:  true,
		},
	}
	clk := &fakeClock{t: time.Now()}

	svc := service.NewPaymentService(&mockOrderRepo{order: *order}, paymentRepo, prov, idgen.New(), clk)
	svc.SetWebhookRepo(webhookRepo)

	err := svc.ProcessWebhook(context.Background(), []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for amount mismatch")
	}
	if webhookRepo.savedPayment.Status() != domain.PaymentFailed {
		t.Errorf("expected FAILED, got %s", webhookRepo.savedPayment.Status())
	}
	if webhookRepo.savedOrder != nil {
		t.Error("expected order not updated on amount mismatch")
	}
}

func TestProcessWebhook_OrphanedPayment(t *testing.T) {
	order := makeTestOrder(t, 15*time.Minute)
	_ = order.Transition(domain.StatusCancelled, "system", "expired", time.Now())

	payment, _ := domain.NewPayment(domain.ID{0x01}, order.ID(), "mock", domain.MethodCard, order.TotalMinor(), "KES", time.Now())
	_ = payment.MarkPending("ref-123", time.Now())

	paymentRepo := &mockPaymentRepo{byRef: payment}
	webhookRepo := &mockWebhookRepo{processed: make(map[string]bool)}
	prov := &mockProvider{
		verifyResult: domain.WebhookEvent{
			ProviderEventID: "evt-123",
			ProviderRef:     "ref-123",
			Status:          domain.PaymentSucceeded,
			AmountMinor:     order.TotalMinor(),
			Currency:        "KES",
			SignatureValid:  true,
		},
	}
	clk := &fakeClock{t: time.Now()}

	svc := service.NewPaymentService(&mockOrderRepo{order: *order}, paymentRepo, prov, idgen.New(), clk)
	svc.SetWebhookRepo(webhookRepo)

	err := svc.ProcessWebhook(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("ProcessWebhook: %v", err)
	}
	if webhookRepo.savedPayment.Status() != domain.PaymentOrphaned {
		t.Errorf("expected ORPHANED, got %s", webhookRepo.savedPayment.Status())
	}
	if webhookRepo.savedOutbox.EventType != "PaymentOrphaned" {
		t.Errorf("expected PaymentOrphaned outbox, got %s", webhookRepo.savedOutbox.EventType)
	}
}
