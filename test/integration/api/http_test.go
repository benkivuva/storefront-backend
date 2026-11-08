package apitest

import (
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/benkivuva/storefront-backend/internal/adapter/hashingadapter"
	httpsrv "github.com/benkivuva/storefront-backend/internal/adapter/http"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/handlers"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres/migrate"
	adapterredis "github.com/benkivuva/storefront-backend/internal/adapter/redis"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/clock"
	"github.com/benkivuva/storefront-backend/internal/pkg/hashing"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
	pkggjwt "github.com/benkivuva/storefront-backend/internal/pkg/jwt"
	"github.com/benkivuva/storefront-backend/internal/service"
)

const (
	testOrigin      = "https://test.example.com"
	testPassword    = "correct-horse-battery-staple"
	testIssuer      = "test-issuer"
	testAudience    = "test-audience"
	authBodyLimit   = 64 << 10
	globalBodyLimit = 1 << 20
)

var (
	sharedPool    *pgxpool.Pool
	sharedRedis   *goredis.Client
	sharedKS      *pkggjwt.KeySet
	sharedSignKey ed25519.PrivateKey
)

func TestMain(m *testing.M) {
	code := runTests(m)
	os.Exit(code)
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	pgCtr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres:", err)
		return 1
	}
	defer func() { _ = pgCtr.Terminate(ctx) }()

	pgURL, err := pgCtr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pg conn string:", err)
		return 1
	}
	if err := migrate.Up(ctx, pgURL); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}

	sharedPool, err = pgxpool.New(ctx, pgURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgxpool:", err)
		return 1
	}
	defer sharedPool.Close()

	redisCtr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections"),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "start redis:", err)
		return 1
	}
	defer func() { _ = redisCtr.Terminate(ctx) }()

	redisHost, _ := redisCtr.Host(ctx)
	redisPort, _ := redisCtr.MappedPort(ctx, "6379")
	sharedRedis = goredis.NewClient(&goredis.Options{Addr: redisHost + ":" + redisPort.Port()})
	defer func() { _ = sharedRedis.Close() }()
	if err := sharedRedis.Ping(ctx).Err(); err != nil {
		fmt.Fprintln(os.Stderr, "redis ping:", err)
		return 1
	}

	seed := make([]byte, ed25519.SeedSize)
	if _, err := crand.Read(seed); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		return 1
	}
	sharedSignKey = ed25519.NewKeyFromSeed(seed)
	sharedKS, err = pkggjwt.NewKeySet("kid-test", sharedSignKey, testIssuer, testAudience, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keyset:", err)
		return 1
	}

	return m.Run()
}

// ---------- per-test stack ----------

type testServer struct {
	URL  string
	Srv  *httptest.Server
	KS   *pkggjwt.KeySet
	Pool *pgxpool.Pool
}

type serverOpts struct {
	AuthLimiter    middleware.RateLimiter
	AuthFallback   middleware.RateLimiter
	GlobalLimiter  middleware.RateLimiter
	GlobalFallback middleware.RateLimiter
	AuthCapacity   int
	GlobalCapacity int
	SecureCookies  bool
}

func newTestServer(t *testing.T, opts serverOpts) *testServer {
	t.Helper()

	if opts.AuthLimiter == nil {
		l, err := adapterredis.NewLimiter(sharedRedis, 1000, time.Minute)
		if err != nil {
			t.Fatalf("auth limiter: %v", err)
		}
		opts.AuthLimiter = &bridgeLimiter{inner: l}
		opts.AuthCapacity = 1000
	}
	if opts.GlobalLimiter == nil {
		l, err := adapterredis.NewLimiter(sharedRedis, 1000, time.Minute)
		if err != nil {
			t.Fatalf("global limiter: %v", err)
		}
		opts.GlobalLimiter = &bridgeLimiter{inner: l}
		opts.GlobalCapacity = 1000
	}
	if opts.AuthCapacity == 0 {
		opts.AuthCapacity = 1000
	}
	if opts.GlobalCapacity == 0 {
		opts.GlobalCapacity = 1000
	}

	userRepo := postgres.NewUserRepo(sharedPool)
	refreshRepo := postgres.NewRefreshTokenRepo(sharedPool)
	addressRepo := postgres.NewAddressRepo(sharedPool)
	auditLog := postgres.NewAuditLog(sharedPool)
	lockout, err := adapterredis.NewLockoutStore(sharedRedis,
		[]adapterredis.BackoffStep{{MinFailures: 3, Wait: 100 * time.Millisecond}},
		24*time.Hour)
	if err != nil {
		t.Fatalf("lockout: %v", err)
	}

	hasher := hashingadapter.NewHasher(hashing.Params{Memory: 8 * 1024, Time: 1, Threads: 1})
	signer := middleware.NewJWTSigner(sharedKS)
	gen := idgen.New()

	svc, err := service.NewAuthService(
		userRepo, refreshRepo, addressRepo, auditLog, lockout,
		hasher, signer, clock.New(), gen, gen,
		service.Config{AccessTTL: 15 * time.Minute, RefreshTTL: 7 * 24 * time.Hour},
	)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}

	trusted := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}

	authH := handlers.NewAuthHandler(svc, trusted, 7*24*time.Hour, opts.SecureCookies)
	meH := handlers.NewMeHandler(svc)

	router := httpsrv.NewPublicRouter(httpsrv.RouterConfig{
		AuthHandler:    authH,
		MeHandler:      meH,
		JWTVerifier:    middleware.NewJWTVerifier(sharedKS),
		AuthLimiter:    opts.AuthLimiter,
		AuthFallback:   opts.AuthFallback,
		GlobalLimiter:  opts.GlobalLimiter,
		GlobalFallback: opts.GlobalFallback,
		TrustedProxies: trusted,
		AllowedOrigins: []string{testOrigin},
		AuthCapacity:   opts.AuthCapacity,
		GlobalCapacity: opts.GlobalCapacity,
		Prod:           false,
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &testServer{URL: srv.URL, Srv: srv, KS: sharedKS, Pool: sharedPool}
}

// bridgeLimiter adapts adapterredis.Limiter to middleware.RateLimiter.
// Duplicated from main.go because main is package main; the alternative is
// a shared bridge package. Duplication is 10 lines.
type bridgeLimiter struct {
	inner interface {
		Allow(ctx context.Context, key string, cost int) (adapterredis.Decision, error)
	}
}

func (b *bridgeLimiter) Allow(ctx context.Context, key string, cost int) (middleware.Decision, error) {
	d, err := b.inner.Allow(ctx, key, cost)
	if err != nil {
		return middleware.Decision{}, err
	}
	return middleware.Decision{Allowed: d.Allowed, Remaining: d.Remaining, RetryAfter: d.RetryAfter}, nil
}

// ---------- request helpers ----------

type httpDoer struct {
	t       *testing.T
	baseURL string
	client  *http.Client
	jar     []*http.Cookie
}

func (d *httpDoer) do(method, path, body, jwt, origin, xff string) (*http.Response, []byte) {
	d.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, d.baseURL+path, rdr)
	if err != nil {
		d.t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	for _, c := range d.jar {
		req.AddCookie(c)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		d.t.Fatalf("http do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	// Update jar from Set-Cookie
	if setCookies := resp.Cookies(); len(setCookies) > 0 {
		d.jar = mergeCookies(d.jar, setCookies)
	}
	return resp, respBody
}

func mergeCookies(existing, incoming []*http.Cookie) []*http.Cookie {
	out := existing
	for _, inc := range incoming {
		found := false
		for i, e := range out {
			if e.Name == inc.Name {
				if inc.MaxAge < 0 {
					out = append(out[:i], out[i+1:]...)
				} else {
					out[i] = inc
				}
				found = true
				break
			}
		}
		if !found && inc.MaxAge >= 0 {
			out = append(out, inc)
		}
	}
	return out
}

func newDoer(t *testing.T, s *testServer) *httpDoer {
	return &httpDoer{
		t:       t,
		baseURL: s.URL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	var b [8]byte
	_, _ = crand.Read(b[:])
	return "t" + strings.ToLower(base64.RawURLEncoding.EncodeToString(b[:])) + "@test.local"
}

var xffCounter int64

func uniqueXFF(t *testing.T) string {
	t.Helper()
	n := atomic.AddInt64(&xffCounter, 1)
	// Produce 10.<high>.<low>.1 so every test gets a unique /24 bucket
	return fmt.Sprintf("10.%d.%d.1", (n>>8)&0xff, n&0xff)
}

func decodeProblem(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal problem: %v (body: %s)", err, body)
	}
	return p
}

func expectStatus(t *testing.T, resp *http.Response, want int, body []byte) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d (body: %s)", resp.StatusCode, want, body)
	}
}

func registerAndLogin(t *testing.T, s *testServer) (*httpDoer, string, string) {
	t.Helper()
	d := newDoer(t, s)
	xff := uniqueXFF(t)
	email := uniqueEmail(t)

	body := fmt.Sprintf(`{"email":%q,"password":%q,"name":"Test"}`, email, testPassword)
	resp, _ := d.do("POST", "/api/v1/auth/register", body, "", "", xff)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", resp.StatusCode)
	}

	loginResp, loginBody := d.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "", "", xff)
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d body=%s", loginResp.StatusCode, loginBody)
	}
	var lr struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(loginBody, &lr)
	return d, lr.AccessToken, email
}

// ---------- Register ----------

func TestHTTP_Register_Success(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	email := uniqueEmail(t)

	body := fmt.Sprintf(`{"email":%q,"password":%q,"name":"Alice"}`, email, testPassword)
	resp, respBody := d.do("POST", "/api/v1/auth/register", body, "", "", uniqueXFF(t))

	expectStatus(t, resp, http.StatusCreated, respBody)
	var r struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	_ = json.Unmarshal(respBody, &r)
	if r.User.Email != email {
		t.Errorf("email = %q", r.User.Email)
	}
	if r.User.ID == "" {
		t.Error("id empty")
	}
}

func TestHTTP_Register_Duplicate_409(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	email := uniqueEmail(t)
	xff := uniqueXFF(t)

	body := fmt.Sprintf(`{"email":%q,"password":%q,"name":""}`, email, testPassword)
	resp, _ := d.do("POST", "/api/v1/auth/register", body, "", "", xff)
	expectStatus(t, resp, http.StatusCreated, nil)

	resp, respBody := d.do("POST", "/api/v1/auth/register", body, "", "", xff)
	expectStatus(t, resp, http.StatusConflict, respBody)
	if decodeProblem(t, respBody)["code"] != "EMAIL_TAKEN" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

func TestHTTP_Register_WeakPassword_400(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)

	body := fmt.Sprintf(`{"email":%q,"password":"short","name":""}`, uniqueEmail(t))
	resp, respBody := d.do("POST", "/api/v1/auth/register", body, "", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusBadRequest, respBody)
	if decodeProblem(t, respBody)["code"] != "PASSWORD_TOO_SHORT" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

func TestHTTP_Register_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	resp, respBody := d.do("POST", "/api/v1/auth/register", "not json", "", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusBadRequest, respBody)
	if decodeProblem(t, respBody)["code"] != "INVALID_REQUEST" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

// ---------- Login ----------

func TestHTTP_Login_WrongPassword_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	xff := uniqueXFF(t)
	email := uniqueEmail(t)

	reg := fmt.Sprintf(`{"email":%q,"password":%q,"name":""}`, email, testPassword)
	d.do("POST", "/api/v1/auth/register", reg, "", "", xff)

	resp, respBody := d.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"wrong"}`, email), "", "", xff)
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
	if decodeProblem(t, respBody)["code"] != "INVALID_CREDENTIALS" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

func TestHTTP_Login_UnknownEmail_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)

	resp, respBody := d.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, uniqueEmail(t), testPassword),
		"", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
	if decodeProblem(t, respBody)["code"] != "INVALID_CREDENTIALS" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

func TestHTTP_Login_DisabledAccount_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, _, email := registerAndLogin(t, s)

	// Deactivate the account directly in DB.
	if _, err := s.Pool.Exec(context.Background(),
		`UPDATE users SET is_active = FALSE WHERE email = $1`, email); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	resp, respBody := d.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword),
		"", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
}

// ---------- /me ----------

func TestHTTP_Me_NoToken_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	resp, respBody := d.do("GET", "/api/v1/me", "", "", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
	if resp.Header.Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("WWW-Authenticate = %q", resp.Header.Get("WWW-Authenticate"))
	}
}

func TestHTTP_Me_ValidToken_200(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, token, email := registerAndLogin(t, s)

	resp, respBody := d.do("GET", "/api/v1/me", "", token, "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusOK, respBody)
	var r struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	_ = json.Unmarshal(respBody, &r)
	if r.User.Email != email {
		t.Errorf("email = %q", r.User.Email)
	}
}

func TestHTTP_Me_WrongSignature_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)

	// Sign with a different key than the server uses.
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	otherKS, _ := pkggjwt.NewKeySet("kid-test", otherPriv, testIssuer, testAudience, nil)
	fakeID := domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	fakeToken, _ := otherKS.Sign(fakeID.String(), string(domain.RoleCustomer), time.Hour)

	resp, respBody := d.do("GET", "/api/v1/me", "", fakeToken, "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
}

// ---------- Refresh ----------

func TestHTTP_Refresh_HappyPath(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, token, _ := registerAndLogin(t, s)

	resp, respBody := d.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, uniqueXFF(t))
	expectStatus(t, resp, http.StatusOK, respBody)
	var r struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(respBody, &r)
	if r.AccessToken == "" || r.AccessToken == token {
		t.Errorf("expected fresh access token, got %q (old %q)", r.AccessToken, token)
	}
}

// THE critical test: replay an already-rotated refresh token. Family must
// be revoked, and the legitimate successor token must also stop working.
func TestHTTP_Refresh_ReplayRevokesFamily(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, _, email := registerAndLogin(t, s)
	xff := uniqueXFF(t)

	// Snapshot cookie A (from login)
	cookiesA := append([]*http.Cookie(nil), d.jar...)
	if len(cookiesA) == 0 {
		t.Fatal("no cookies from login")
	}

	// Rotate: A -> B
	resp, respBody := d.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, xff)
	expectStatus(t, resp, http.StatusOK, respBody)
	cookiesB := append([]*http.Cookie(nil), d.jar...)

	// Query family_id from DB via email lookup
	var userID domain.ID
	var familyID string
	err := s.Pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`, email).Scan((*[16]byte)(&userID))
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	err = s.Pool.QueryRow(context.Background(),
		`SELECT family_id::text FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL`,
		[16]byte(userID)).Scan(&familyID)
	if err != nil {
		t.Fatalf("find family: %v", err)
	}

	// Replay A: attacker presents the old cookie after legitimate rotation.
	dA := newDoer(t, s)
	dA.jar = cookiesA
	resp, respBody = dA.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, xff)
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
	if decodeProblem(t, respBody)["code"] != "TOKEN_REUSED" {
		t.Errorf("code = %v, want TOKEN_REUSED", decodeProblem(t, respBody)["code"])
	}

	// Now B (the legitimate successor) must also fail — the whole family was revoked.
	dB := newDoer(t, s)
	dB.jar = cookiesB
	resp, respBody = dB.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, xff)
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
	if decodeProblem(t, respBody)["code"] != "TOKEN_REUSED" {
		t.Errorf("successor code = %v, want TOKEN_REUSED", decodeProblem(t, respBody)["code"])
	}

	// DB check: every token in the family has revoked_at set.
	var activeCount int
	err = s.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM refresh_tokens
 WHERE family_id = $1::uuid AND revoked_at IS NULL`,
		familyID).Scan(&activeCount)
	if err != nil {
		t.Fatalf("count active: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("active tokens in family = %d, want 0", activeCount)
	}

	// Audit check: refresh_reuse_detected logged.
	var auditCount int
	_ = s.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log
 WHERE action = 'refresh_reuse_detected' AND actor = $1`,
		[16]byte(userID)).Scan(&auditCount)
	if auditCount < 1 {
		t.Errorf("expected ≥1 reuse-detected audit row, got %d", auditCount)
	}
}

func TestHTTP_Refresh_MissingCookie_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	resp, respBody := d.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, uniqueXFF(t))
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
}

// ---------- Logout ----------

func TestHTTP_Logout_ThenRefresh_Fails(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, _, _ := registerAndLogin(t, s)
	xff := uniqueXFF(t)

	resp, _ := d.do("POST", "/api/v1/auth/logout", "", "", testOrigin, xff)
	expectStatus(t, resp, http.StatusNoContent, nil)

	// Cookie should have been cleared, jar should have no refresh_token
	for _, c := range d.jar {
		if c.Name == "refresh_token" {
			t.Fatalf("refresh_token cookie still present after logout")
		}
	}

	resp, respBody := d.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, xff)
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
}

// ---------- Rate limiting ----------

func TestHTTP_AuthRateLimit_429(t *testing.T) {
	t.Parallel()
	limitedLimiter, _ := adapterredis.NewLimiter(sharedRedis, 2, time.Minute)
	s := newTestServer(t, serverOpts{
		AuthLimiter:  &bridgeLimiter{inner: limitedLimiter},
		AuthCapacity: 2,
	})
	d := newDoer(t, s)
	xff := uniqueXFF(t) // bucket is anon:<xff-prefix>

	// Two login attempts allowed (will fail auth but consume quota).
	for i := 0; i < 2; i++ {
		d.do("POST", "/api/v1/auth/login",
			fmt.Sprintf(`{"email":%q,"password":"x"}`, uniqueEmail(t)), "", "", xff)
	}

	// Third should be rate limited.
	resp, respBody := d.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"x"}`, uniqueEmail(t)), "", "", xff)
	expectStatus(t, resp, http.StatusTooManyRequests, respBody)
	if resp.Header.Get("Retry-After") == "" {
		t.Error("Retry-After missing on 429")
	}
	if decodeProblem(t, respBody)["code"] != "RATE_LIMITED" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

// ---------- Redis down ----------

func TestHTTP_RedisDown_NoFallback_503(t *testing.T) {
	t.Parallel()
	deadRedis := goredis.NewClient(&goredis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  0,
	})
	defer func() { _ = deadRedis.Close() }()

	deadLimiter, _ := adapterredis.NewLimiter(deadRedis, 5, time.Minute)
	s := newTestServer(t, serverOpts{
		AuthLimiter:  &bridgeLimiter{inner: deadLimiter},
		AuthCapacity: 5,
		// No fallback → 503
	})
	d := newDoer(t, s)

	resp, respBody := d.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"x"}`, uniqueEmail(t)), "", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusServiceUnavailable, respBody)
	if decodeProblem(t, respBody)["code"] != "RATE_LIMITER_UNAVAILABLE" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

func TestHTTP_RedisDown_WithFallback_EnforcesLimit(t *testing.T) {
	t.Parallel()
	deadRedis := goredis.NewClient(&goredis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  0,
	})
	defer func() { _ = deadRedis.Close() }()

	deadLimiter, _ := adapterredis.NewLimiter(deadRedis, 2, time.Minute)
	fallback, _ := adapterredis.NewMemoryLimiter(2, time.Minute)
	s := newTestServer(t, serverOpts{
		AuthLimiter:  &bridgeLimiter{inner: deadLimiter},
		AuthFallback: &bridgeLimiter{inner: fallback},
		AuthCapacity: 2,
	})
	d := newDoer(t, s)
	xff := uniqueXFF(t)

	// Two allowed by memory fallback.
	for i := 0; i < 2; i++ {
		resp, _ := d.do("POST", "/api/v1/auth/login",
			fmt.Sprintf(`{"email":%q,"password":"x"}`, uniqueEmail(t)), "", "", xff)
		if resp.StatusCode == http.StatusServiceUnavailable {
			t.Fatalf("iteration %d: fallback not used (503)", i)
		}
	}

	// Third must be rate-limited by memory fallback.
	resp, respBody := d.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"x"}`, uniqueEmail(t)), "", "", xff)
	expectStatus(t, resp, http.StatusTooManyRequests, respBody)
}

// ---------- CORS + Origin ----------

func TestHTTP_CORS_Preflight_Allowed(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	req, _ := http.NewRequest("OPTIONS", d.baseURL+"/api/v1/auth/login", nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := d.client.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != testOrigin {
		t.Errorf("ACAO = %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestHTTP_CORS_Preflight_Disallowed_403(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	req, _ := http.NewRequest("OPTIONS", d.baseURL+"/api/v1/auth/login", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := d.client.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHTTP_OriginCheck_DisallowedOrigin_403(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, _, _ := registerAndLogin(t, s)

	resp, respBody := d.do("POST", "/api/v1/auth/refresh", "",
		"", "https://evil.example.com", uniqueXFF(t))
	expectStatus(t, resp, http.StatusForbidden, respBody)
}

// ---------- Body limits ----------

func TestHTTP_BodyLimit_AuthEndpoint_413(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)

	// 100KB body exceeds the 64KB auth limit.
	bigName := strings.Repeat("a", authBodyLimit+100)
	body := fmt.Sprintf(`{"email":%q,"password":%q,"name":%q}`, uniqueEmail(t), testPassword, bigName)
	resp, respBody := d.do("POST", "/api/v1/auth/register", body, "", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusRequestEntityTooLarge, respBody)
	if decodeProblem(t, respBody)["code"] != "BODY_TOO_LARGE" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

// ---------- Profile + addresses + sessions ----------

func TestHTTP_UpdateMe_EmptyPatch_400(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, token, _ := registerAndLogin(t, s)

	resp, respBody := d.do("PATCH", "/api/v1/me", `{}`, token, "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusBadRequest, respBody)
	if decodeProblem(t, respBody)["code"] != "EMPTY_PATCH" {
		t.Errorf("code = %v", decodeProblem(t, respBody)["code"])
	}
}

func TestHTTP_UpdateMe_Success(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, token, _ := registerAndLogin(t, s)

	resp, respBody := d.do("PATCH", "/api/v1/me", `{"name":"Updated Name"}`, token, "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusOK, respBody)
	var r struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	_ = json.Unmarshal(respBody, &r)
	if r.User.Name != "Updated Name" {
		t.Errorf("name = %q", r.User.Name)
	}
}

func TestHTTP_Addresses_CreateAndList(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, token, _ := registerAndLogin(t, s)
	xff := uniqueXFF(t)

	body := `{"recipient":"Jane","phone":"+254712345678","line1":"12 Kimathi","town":"Nairobi"}`
	resp, respBody := d.do("POST", "/api/v1/me/addresses", body, token, "", xff)
	expectStatus(t, resp, http.StatusCreated, respBody)

	resp, respBody = d.do("GET", "/api/v1/me/addresses", "", token, "", xff)
	expectStatus(t, resp, http.StatusOK, respBody)
	var r struct {
		Addresses []struct {
			Recipient string `json:"recipient"`
		} `json:"addresses"`
	}
	_ = json.Unmarshal(respBody, &r)
	if len(r.Addresses) != 1 || r.Addresses[0].Recipient != "Jane" {
		t.Fatalf("addresses = %+v", r.Addresses)
	}
}

func TestHTTP_Sessions_ListAndRevoke(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, token, _ := registerAndLogin(t, s)
	xff := uniqueXFF(t)

	resp, respBody := d.do("GET", "/api/v1/me/sessions", "", token, "", xff)
	expectStatus(t, resp, http.StatusOK, respBody)
	var r struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal(respBody, &r)
	if len(r.Sessions) == 0 {
		t.Fatal("expected at least one session")
	}

	sessionID := r.Sessions[0].ID
	resp, respBody = d.do("DELETE", "/api/v1/me/sessions/"+sessionID, "", token, "", xff)
	expectStatus(t, resp, http.StatusNoContent, respBody)
}

// ---------- misc ----------

func TestHTTP_NotFound(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)
	resp, respBody := d.do("GET", "/nonexistent", "", "", "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusNotFound, respBody)
}

func TestHTTP_RequestIDPresent(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)

	req, _ := http.NewRequest("POST", d.baseURL+"/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", uniqueXFF(t))
	resp, err := d.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing")
	}
}

func TestHTTP_Refresh_Concurrent(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, _, _ := registerAndLogin(t, s)
	xff := uniqueXFF(t)

	// Snapshot the cookie from login
	cookies := append([]*http.Cookie(nil), d.jar...)

	var wg sync.WaitGroup
	const N = 5
	results := make([]int, N)

	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			gd := newDoer(t, s)
			// Deep-copy each cookie so goroutines don't share memory.
			gd.jar = make([]*http.Cookie, len(cookies))
			for j, c := range cookies {
				cc := *c
				gd.jar[j] = &cc
			}
			<-start
			resp, _ := gd.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, xff)
			results[idx] = resp.StatusCode
		}(i)
	}
	close(start)
	wg.Wait()

	success, reused := 0, 0
	for _, code := range results {
		switch code {
		case http.StatusOK:
			success++
		case http.StatusUnauthorized:
			reused++
		}
	}

	if success != 1 || reused != 4 {
		t.Fatalf("expected 1 success and 4 reused, got %d success and %d reused (codes: %v)", success, reused, results)
	}
}

func TestHTTP_Lockout_EndToEnd(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, _, email := registerAndLogin(t, s)
	xff := uniqueXFF(t)

	// The test server lockout schedule is MinFailures: 3, Wait: 100ms.
	// 3 wrong passwords.
	for i := 0; i < 3; i++ {
		d.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"wrong"}`, email), "", "", xff)
	}

	// Correct password while locked should fail.
	resp, respBody := d.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "", "", xff)
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
	if decodeProblem(t, respBody)["code"] != "INVALID_CREDENTIALS" {
		t.Errorf("expected INVALID_CREDENTIALS during lockout, got %v", decodeProblem(t, respBody)["code"])
	}

	// Wait for the lockout to expire (200ms to avoid flakes).
	time.Sleep(200 * time.Millisecond)

	// Correct password now should succeed.
	resp, respBody = d.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "", "", xff)
	expectStatus(t, resp, http.StatusOK, respBody)
}

func TestHTTP_Logout_ServerSideReplay(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d, _, _ := registerAndLogin(t, s)
	xff := uniqueXFF(t)

	// Snapshot cookie
	cookies := append([]*http.Cookie(nil), d.jar...)

	// Logout
	resp, _ := d.do("POST", "/api/v1/auth/logout", "", "", testOrigin, xff)
	expectStatus(t, resp, http.StatusNoContent, nil)

	// Attacker replays the old cookie.
	ad := newDoer(t, s)
	ad.jar = cookies
	resp, respBody := ad.do("POST", "/api/v1/auth/refresh", "", "", testOrigin, xff)

	// Must be 401 (family revoked).
	expectStatus(t, resp, http.StatusUnauthorized, respBody)
}

func TestHTTP_CookieFlags(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{SecureCookies: true})
	d := newDoer(t, s)
	xff := uniqueXFF(t)
	email := uniqueEmail(t)

	reg := fmt.Sprintf(`{"email":%q,"password":%q,"name":""}`, email, testPassword)
	d.do("POST", "/api/v1/auth/register", reg, "", "", xff)

	resp, _ := d.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "", "", xff)
	expectStatus(t, resp, http.StatusOK, nil)

	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookies returned")
	}

	var refreshCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			refreshCookie = c
		}
	}
	if refreshCookie == nil {
		t.Fatal("refresh_token cookie missing")
	}

	if !refreshCookie.HttpOnly {
		t.Error("HttpOnly must be true")
	}
	if !refreshCookie.Secure {
		t.Error("Secure must be true when configured")
	}
	if refreshCookie.SameSite != http.SameSiteStrictMode {
		t.Error("SameSite must be Strict")
	}
	if refreshCookie.Path != "/api/v1/auth" {
		t.Errorf("Path = %q", refreshCookie.Path)
	}
}

func TestHTTP_JWT_EdgeCases(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)

	signID := domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	t.Run("alg none", func(t *testing.T) {
		token := golangjwt.NewWithClaims(golangjwt.SigningMethodNone, golangjwt.RegisteredClaims{
			Subject:   signID.String(),
			Issuer:    testIssuer,
			Audience:  golangjwt.ClaimStrings{testAudience},
			ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(time.Hour)),
		})
		raw, _ := token.SignedString(golangjwt.UnsafeAllowNoneSignatureType)
		resp, _ := d.do("GET", "/api/v1/me", "", raw, "", uniqueXFF(t))
		expectStatus(t, resp, http.StatusUnauthorized, nil)
	})

	t.Run("wrong audience", func(t *testing.T) {
		claims := golangjwt.RegisteredClaims{
			Subject:   signID.String(),
			Issuer:    testIssuer,
			Audience:  golangjwt.ClaimStrings{"wrong-audience"},
			ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(time.Hour)),
		}
		token := golangjwt.NewWithClaims(golangjwt.SigningMethodEdDSA, claims)
		token.Header["kid"] = "kid-test"
		raw, _ := token.SignedString(sharedSignKey)
		resp, _ := d.do("GET", "/api/v1/me", "", raw, "", uniqueXFF(t))
		expectStatus(t, resp, http.StatusUnauthorized, nil)
	})

	t.Run("expired", func(t *testing.T) {
		claims := golangjwt.RegisteredClaims{
			Subject:   signID.String(),
			Issuer:    testIssuer,
			Audience:  golangjwt.ClaimStrings{testAudience},
			ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(-time.Hour)),
		}
		token := golangjwt.NewWithClaims(golangjwt.SigningMethodEdDSA, claims)
		token.Header["kid"] = "kid-test"
		raw, _ := token.SignedString(sharedSignKey)
		resp, _ := d.do("GET", "/api/v1/me", "", raw, "", uniqueXFF(t))
		expectStatus(t, resp, http.StatusUnauthorized, nil)
	})
}

func TestHTTP_JWT_TamperedPayload(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	d := newDoer(t, s)

	// 1. Get a valid token
	fakeID := domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	raw, _ := sharedKS.Sign(fakeID.String(), string(domain.RoleCustomer), time.Hour)

	// 2. Tamper with the payload (change customer to admin)
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatal("invalid token format")
	}

	// Decode payload, change role, re-encode
	payloadBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	_ = json.Unmarshal(payloadBytes, &claims)
	claims["role"] = "admin"
	newPayloadBytes, _ := json.Marshal(claims)
	parts[1] = base64.RawURLEncoding.EncodeToString(newPayloadBytes)

	// 3. Reassemble (keeping the original signature)
	tamperedToken := strings.Join(parts, ".")

	// 4. Send the tampered token
	resp, _ := d.do("GET", "/api/v1/me", "", tamperedToken, "", uniqueXFF(t))
	expectStatus(t, resp, http.StatusUnauthorized, nil)
}
