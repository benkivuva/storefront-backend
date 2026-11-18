package apitest

import (
"context"
"encoding/json"
"fmt"
"net/http"
"net/http/httptest"
"strings"
"testing"
"time"

"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
adapterredis "github.com/benkivuva/storefront-backend/internal/adapter/redis"
"github.com/benkivuva/storefront-backend/internal/domain"
"github.com/benkivuva/storefront-backend/internal/service"
)

type channelMailer struct {
Ch chan domain.MailMessage
}

func (m *channelMailer) Send(ctx context.Context, msg domain.MailMessage) error {
m.Ch <- msg
return nil
}

func TestE2E_EventingPipeline(t *testing.T) {
s := newTestServer(t, serverOpts{})
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

err := s.Redis.XGroupCreateMkStream(ctx, "events:order", "e2e-group", "0").Err()
if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
t.Fatalf("create consumer group: %v", err)
}

outboxRepo := postgres.NewOutboxRepo(s.Pool)
processedRepo := postgres.NewProcessedEventRepo(s.Pool)
publisher := adapterredis.NewEventPublisher(s.Redis, "events")
reader := adapterredis.NewStreamReader(s.Redis, "events:order", "events:dlq")
userRepo := postgres.NewUserRepo(s.Pool)

mailCh := make(chan domain.MailMessage, 1)
mailer := &channelMailer{Ch: mailCh}

emailHandler := service.NewEmailHandler(mailer, userRepo)
relay := service.NewOutboxRelay(outboxRepo, publisher)
consumer := service.NewConsumer("e2e-group", "e2e-worker", reader, processedRepo, emailHandler)

go func() {
ticker := time.NewTicker(50 * time.Millisecond)
defer ticker.Stop()
for {
select {
case <-ctx.Done():
return
case <-ticker.C:
_ = relay.ProcessBatch(ctx, 100)
}
}
}()

go func() {
for {
select {
case <-ctx.Done():
return
default:
_ = consumer.ProcessNextBatch(ctx, 10)
}
}
}()

// Setup Admin
adminEmail := uniqueEmail(t)
adminXFF := uniqueXFF(t)
adminD := newDoer(t, s)
adminD.do("POST", "/api/v1/auth/register", fmt.Sprintf(`{"email":%q,"password":%q,"name":"Admin"}`, adminEmail, testPassword), "", "", adminXFF)
_, _ = s.Pool.Exec(ctx, `UPDATE users SET role = 'admin' WHERE email = $1`, adminEmail)

resp, loginBody := adminD.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, adminEmail, testPassword), "", "", adminXFF)
if resp.StatusCode != http.StatusOK { t.Fatalf("admin login: %d", resp.StatusCode) }
var adminLogin struct{ AccessToken string `json:"access_token"` }
_ = json.Unmarshal(loginBody, &adminLogin)

sku := "E2E-" + uniqueXFF(t)
createBody := fmt.Sprintf(`{"sku":%q,"name":"E2E Item","price_minor":10000,"tax_class":"STANDARD_16"}`, sku)
req := httptest.NewRequest("POST", s.URL+"/api/v1/admin/products", strings.NewReader(createBody))
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer "+adminLogin.AccessToken)
req.Header.Set("X-Forwarded-For", adminXFF)
rec := httptest.NewRecorder()
s.Srv.Config.Handler.ServeHTTP(rec, req)

var product struct{ ID string `json:"id"` }
_ = json.Unmarshal(rec.Body.Bytes(), &product)

stockBody := `{"delta":10,"reason":"RESTOCK"}`
req = httptest.NewRequest("POST", s.URL+"/api/v1/admin/products/"+product.ID+"/inventory", strings.NewReader(stockBody))
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer "+adminLogin.AccessToken)
req.Header.Set("X-Forwarded-For", adminXFF)
s.Srv.Config.Handler.ServeHTTP(httptest.NewRecorder(), req)

// Setup Customer
custEmail := uniqueEmail(t)
custXFF := uniqueXFF(t)
custD := newDoer(t, s)
custD.do("POST", "/api/v1/auth/register", fmt.Sprintf(`{"email":%q,"password":%q,"name":"Cust"}`, custEmail, testPassword), "", "", custXFF)
_, custLoginBody := custD.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, custEmail, testPassword), "", "", custXFF)
var custLogin struct{ AccessToken string `json:"access_token"` }
_ = json.Unmarshal(custLoginBody, &custLogin)

_, addrBody := custD.do("POST", "/api/v1/me/addresses", `{"recipient":"Cust","phone":"+254712345678","line1":"1 St","town":"Nairobi"}`, custLogin.AccessToken, "", custXFF)
var addr struct{ ID string `json:"id"` }
_ = json.Unmarshal(addrBody, &addr)

custD.do("POST", "/api/v1/cart/items", fmt.Sprintf(`{"product_id":%q,"quantity":1}`, product.ID), custLogin.AccessToken, "", custXFF)

// Trigger Checkout
checkoutBody := fmt.Sprintf(`{"address_id":%q,"expected_total_minor":60000}`, addr.ID)
idemKey := "e2e-" + uniqueXFF(t)

req = httptest.NewRequest("POST", s.URL+"/api/v1/checkout", strings.NewReader(checkoutBody))
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer "+custLogin.AccessToken)
req.Header.Set("Idempotency-Key", idemKey)
req.Header.Set("X-Forwarded-For", custXFF)
rec = httptest.NewRecorder()
s.Srv.Config.Handler.ServeHTTP(rec, req)

if rec.Code != http.StatusCreated {
t.Fatalf("checkout failed: %d body=%s", rec.Code, rec.Body.String())
}

// Wait for Email
select {
case msg := <-mailCh:
if msg.To != custEmail {
t.Errorf("email sent to wrong user: got %s, want %s", msg.To, custEmail)
}
case <-time.After(5 * time.Second):
t.Fatal("timeout: did not receive email within 5 seconds.")
}

// Poll database for up to 2 seconds to allow the consumer to finish writing
var outboxStatus string
var processedCount int
deadline := time.Now().Add(2 * time.Second)

for time.Now().Before(deadline) {
_ = s.Pool.QueryRow(ctx, `SELECT status FROM outbox_events WHERE event_type = 'OrderCreated' ORDER BY created_at DESC LIMIT 1`).Scan(&outboxStatus)
_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM processed_events WHERE consumer = 'e2e-group'`).Scan(&processedCount)

if outboxStatus == "published" && processedCount == 1 {
break
}
time.Sleep(10 * time.Millisecond)
}

if outboxStatus != "published" {
t.Fatalf("expected outbox status 'published', got %s", outboxStatus)
}
if processedCount != 1 {
t.Fatalf("expected 1 processed event, got %d", processedCount)
}
}
