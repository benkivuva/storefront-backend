package integration

import (
	"context"
	"crypto/rand"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres/migrate"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

type testEnv struct {
	pool      *pgxpool.Pool
	users     *postgres.UserRepo
	refresh   *postgres.RefreshTokenRepo
	addresses *postgres.AddressRepo
	audit     *postgres.AuditLog
}

func setupAuthEnv(t *testing.T) *testEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	if err := migrate.Up(ctx, url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	return &testEnv{
		pool:      pool,
		users:     postgres.NewUserRepo(pool),
		refresh:   postgres.NewRefreshTokenRepo(pool),
		addresses: postgres.NewAddressRepo(pool),
		audit:     postgres.NewAuditLog(pool),
	}
}

func newID(t *testing.T) domain.ID {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return domain.ID(b)
}

func newEmail(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return "u" + domain.ID([16]byte{b[0], b[1], b[2], b[3], b[4], b[5], 0x70, 0x7f, b[6], b[7], 0, 0, 0, 0, 0, 1}).String() + "@test.local"
}

func newUser(t *testing.T, env *testEnv, email string) domain.User {
	t.Helper()
	u, err := domain.NewUser(newID(t), email, "hash", "Test", domain.RoleCustomer, "", time.Now().UTC())
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if err := env.users.Create(context.Background(), u); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	return u
}

// ---------- UserRepo ----------

func TestUserRepo_CreateFind(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	email := newEmail(t)

	created := newUser(t, env, email)

	byEmail, err := env.users.FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if byEmail.ID() != created.ID() {
		t.Errorf("FindByEmail id mismatch")
	}
	if byEmail.Email() != email {
		t.Errorf("Email = %q", byEmail.Email())
	}
	if !byEmail.IsActive() {
		t.Error("user should be active")
	}
	if byEmail.IsEmailVerified() {
		t.Error("user should not be email-verified")
	}

	byID, err := env.users.FindByID(ctx, created.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if byID.Email() != email {
		t.Errorf("FindByID email mismatch")
	}
}

func TestUserRepo_FindNotFound(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()

	if _, err := env.users.FindByID(ctx, newID(t)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("FindByID: want ErrNotFound, got %v", err)
	}
	if _, err := env.users.FindByEmail(ctx, "nobody@test.local"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("FindByEmail: want ErrNotFound, got %v", err)
	}
}

func TestUserRepo_DuplicateEmail_Returns_ErrConflict(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	email := newEmail(t)

	newUser(t, env, email)

	dup, err := domain.NewUser(newID(t), email, "hash2", "Other", domain.RoleCustomer, "", time.Now().UTC())
	if err != nil {
		t.Fatalf("NewUser dup: %v", err)
	}
	if err := env.users.Create(ctx, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict on duplicate email, got %v", err)
	}
}

func TestUserRepo_UpdateProfile_WritesOnlyProfile(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	email := newEmail(t)
	u := newUser(t, env, email)

	name := "Updated Name"
	at := time.Now().UTC()
	if err := env.users.UpdateProfile(ctx, u.ID(), &name, nil, at); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	got, _ := env.users.FindByID(ctx, u.ID())
	if got.Name() != name {
		t.Errorf("Name = %q, want %q", got.Name(), name)
	}
	if got.PasswordHash() != "hash" {
		t.Errorf("PasswordHash = %q, want unchanged 'hash'", got.PasswordHash())
	}
	if got.Role() != domain.RoleCustomer {
		t.Errorf("Role = %q", got.Role())
	}
}

func TestUserRepo_UpdatePasswordHash_WritesOnlyHash(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	email := newEmail(t)

	u0, _ := domain.NewUser(newID(t), email, "old-hash", "Original", domain.RoleCustomer, "", time.Now().UTC())
	if err := env.users.Create(ctx, u0); err != nil {
		t.Fatal(err)
	}

	at := time.Now().UTC()
	if err := env.users.UpdatePasswordHash(ctx, u0.ID(), "new-hash", at); err != nil {
		t.Fatalf("UpdatePasswordHash: %v", err)
	}

	got, _ := env.users.FindByID(ctx, u0.ID())
	if got.PasswordHash() != "new-hash" {
		t.Errorf("PasswordHash = %q, want new-hash", got.PasswordHash())
	}
	if got.Name() != "Original" {
		t.Errorf("Name = %q, want unchanged 'Original'", got.Name())
	}
}

func TestUserRepo_UpdateProfile_NotFound(t *testing.T) {
	env := setupAuthEnv(t)
	name := "X"
	if err := env.users.UpdateProfile(context.Background(), newID(t), &name, nil, time.Now()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// ---------- RefreshTokenRepo ----------

func insertParent(t *testing.T, env *testEnv, u domain.User) domain.RefreshToken {
	t.Helper()
	now := time.Now().UTC()
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	tok, err := domain.NewRefreshToken(
		newID(t), u.ID(), newID(t),
		domain.HashRefreshToken(raw[:]),
		now.Add(7*24*time.Hour), now,
		"test-ua", "1.2.3.4",
	)
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	if err := env.refresh.Create(context.Background(), tok); err != nil {
		t.Fatalf("Create refresh: %v", err)
	}
	return tok
}

func TestRefreshRepo_IPRoundTrip(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u := newUser(t, env, newEmail(t))
	parent := insertParent(t, env, u)

	got, err := env.refresh.FindByHash(ctx, parent.TokenHash())
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if got.IP() != "1.2.3.4" {
		t.Errorf("IP = %q, want 1.2.3.4", got.IP())
	}
	if got.UserAgent() != "test-ua" {
		t.Errorf("UserAgent = %q", got.UserAgent())
	}
}

func TestRefreshRepo_IPEmptyStoredAsNull(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u := newUser(t, env, newEmail(t))

	now := time.Now().UTC()
	var raw [32]byte
	_, _ = rand.Read(raw[:])
	tok, err := domain.NewRefreshToken(
		newID(t), u.ID(), newID(t),
		domain.HashRefreshToken(raw[:]),
		now.Add(24*time.Hour), now, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.refresh.Create(ctx, tok); err != nil {
		t.Fatalf("Create with empty IP: %v", err)
	}

	got, _ := env.refresh.FindByHash(ctx, tok.TokenHash())
	if got.IP() != "" {
		t.Errorf("IP = %q, want empty (NULL round-trip)", got.IP())
	}
}

// Rotate under concurrent contention: two goroutines on the same parent.
// Exactly one nil, one ErrConflict, exactly one child row.
func TestRefreshRepo_Rotate_Concurrent_ExactlyOneSucceeds(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u := newUser(t, env, newEmail(t))
	parent := insertParent(t, env, u)

	now := time.Now().UTC()
	exp := now.Add(7 * 24 * time.Hour)

	var raw1, raw2 [32]byte
	_, _ = rand.Read(raw1[:])
	_, _ = rand.Read(raw2[:])

	parentAfter1, child1, err := parent.Rotate(newID(t), domain.HashRefreshToken(raw1[:]), exp, now)
	if err != nil {
		t.Fatal(err)
	}
	parentAfter2, child2, err := parent.Rotate(newID(t), domain.HashRefreshToken(raw2[:]), exp, now)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	start := make(chan struct{})
	errs := make([]error, 2)

	go func() { defer wg.Done(); <-start; errs[0] = env.refresh.Rotate(ctx, parentAfter1, child1) }()
	go func() { defer wg.Done(); <-start; errs[1] = env.refresh.Rotate(ctx, parentAfter2, child2) }()
	close(start)
	wg.Wait()

	successes, conflicts := 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			successes++
		case errors.Is(e, domain.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1 and 1", successes, conflicts)
	}

	var childCount int
	if err := env.pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE family_id = $1 AND id != $2`,
		[16]byte(parent.FamilyID()), [16]byte(parent.ID())).Scan(&childCount); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if childCount != 1 {
		t.Fatalf("child rows = %d, want 1 (the loser must have rolled back)", childCount)
	}

	got, _ := env.refresh.FindByHash(ctx, parent.TokenHash())
	if !got.IsRevoked() {
		t.Fatal("parent should be revoked after successful rotation")
	}
	if got.ReplacedBy().IsZero() {
		t.Fatal("parent replaced_by not set")
	}
}

func TestRefreshRepo_Rotate_MissingParent_ErrConflict(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()

	now := time.Now().UTC()
	var raw [32]byte
	_, _ = rand.Read(raw[:])
	ghost, err := domain.NewRefreshToken(
		newID(t), newID(t), newID(t),
		domain.HashRefreshToken(raw[:]),
		now.Add(time.Hour), now, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	parentAfter, child, err := ghost.Rotate(newID(t), domain.HashRefreshToken([]byte("child")), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.refresh.Rotate(ctx, parentAfter, child); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict on missing parent, got %v", err)
	}
}

func TestRefreshRepo_RevokeFamily_CountsOnlyUnrevoked(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u := newUser(t, env, newEmail(t))
	parent := insertParent(t, env, u)

	n, err := env.refresh.RevokeFamily(ctx, parent.FamilyID(), time.Now().UTC())
	if err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}
	if n != 1 {
		t.Errorf("first call revoked %d rows, want 1", n)
	}

	n2, err := env.refresh.RevokeFamily(ctx, parent.FamilyID(), time.Now().UTC())
	if err != nil {
		t.Fatalf("RevokeFamily second: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second call revoked %d rows, want 0 (already revoked)", n2)
	}
}

func TestRefreshRepo_ListByUser_FiltersByUser(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u1 := newUser(t, env, newEmail(t))
	u2 := newUser(t, env, newEmail(t))
	insertParent(t, env, u1)
	insertParent(t, env, u1)
	insertParent(t, env, u2)

	got, err := env.refresh.ListByUser(ctx, u1.ID())
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d tokens for u1, want 2", len(got))
	}
}

func TestRefreshRepo_RevokeByUser_RejectsForeignToken(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u1 := newUser(t, env, newEmail(t))
	u2 := newUser(t, env, newEmail(t))
	tok := insertParent(t, env, u1)

	if err := env.refresh.RevokeByUser(ctx, u2.ID(), tok.ID(), time.Now().UTC()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound when revoking another user's token, got %v", err)
	}
}

// ---------- AddressRepo ----------

func TestAddressRepo_CreateAndList(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u := newUser(t, env, newEmail(t))

	now := time.Now().UTC()
	a, err := domain.NewAddress(
		newID(t), u.ID(),
		"Jane", "+254712345678",
		"12 Kimathi", "Apt 4", "Nairobi", "Nairobi", "note",
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.addresses.Create(ctx, a); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := env.addresses.ListByUser(ctx, u.ID())
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Recipient() != "Jane" || got[0].Town() != "Nairobi" || got[0].Notes() != "note" {
		t.Errorf("roundtrip mismatch: %+v", got[0])
	}
}

// ---------- AuditLog ----------

func TestAuditLog_AppendAndReadBack(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u := newUser(t, env, newEmail(t))

	entry := domain.AuditEntry{
		Actor:      u.ID(),
		Action:     "test_action",
		EntityType: "user",
		EntityID:   u.ID().String(),
		Before:     []byte(`{"name_changed":true}`),
		After:      []byte(`{"phone_changed":true}`),
		IP:         netip.MustParseAddr("1.2.3.4"),
		At:         time.Now().UTC(),
	}
	if err := env.audit.Append(ctx, entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var (
		action, entityType, entityID string
		before, after                []byte
		ip                           *netip.Addr
	)
	if err := env.pool.QueryRow(ctx,
		`SELECT action, entity_type, entity_id, before, after, ip
 FROM audit_log WHERE actor = $1`,
		[16]byte(u.ID())).Scan(&action, &entityType, &entityID, &before, &after, &ip); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if action != "test_action" || entityType != "user" {
		t.Errorf("action/entity mismatch: %s / %s", action, entityType)
	}
	if ip == nil || ip.String() != "1.2.3.4" {
		t.Errorf("ip = %v, want 1.2.3.4", ip)
	}
}

func TestAuditLog_NullIP(t *testing.T) {
	env := setupAuthEnv(t)
	ctx := context.Background()
	u := newUser(t, env, newEmail(t))

	entry := domain.AuditEntry{
		Actor:      u.ID(),
		Action:     "no_ip_action",
		EntityType: "user",
		EntityID:   u.ID().String(),
		At:         time.Now().UTC(),
	}
	if err := env.audit.Append(ctx, entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var ip *netip.Addr
	if err := env.pool.QueryRow(ctx,
		`SELECT ip FROM audit_log WHERE actor = $1`, [16]byte(u.ID())).Scan(&ip); err != nil {
		t.Fatalf("query: %v", err)
	}
	if ip != nil {
		t.Errorf("ip = %v, want NULL", ip)
	}
}
