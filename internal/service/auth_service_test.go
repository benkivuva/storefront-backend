package service

import (
"context"
"errors"
"fmt"
"net/netip"
"sync"
"testing"
"time"

"github.com/benkivuva/storefront-backend/internal/domain"
)

// ---------- fakes ----------

type fakeUserRepo struct {
mu                      sync.Mutex
users                   map[domain.ID]domain.User
byEmail                 map[string]domain.ID
createErr               error
findErr                 error
updateProfileCalls      int
updatePasswordHashCalls int
}

func newFakeUserRepo() *fakeUserRepo {
return &fakeUserRepo{
users:   map[domain.ID]domain.User{},
byEmail: map[string]domain.ID{},
}
}

func (r *fakeUserRepo) Create(_ context.Context, u domain.User) error {
r.mu.Lock()
defer r.mu.Unlock()
if r.createErr != nil {
return r.createErr
}
if _, exists := r.byEmail[u.Email()]; exists {
return domain.ErrConflict
}
r.users[u.ID()] = u
r.byEmail[u.Email()] = u.ID()
return nil
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (domain.User, error) {
r.mu.Lock()
defer r.mu.Unlock()
if r.findErr != nil {
return domain.User{}, r.findErr
}
id, ok := r.byEmail[email]
if !ok {
return domain.User{}, domain.ErrNotFound
}
return r.users[id], nil
}

func (r *fakeUserRepo) FindByID(_ context.Context, id domain.ID) (domain.User, error) {
r.mu.Lock()
defer r.mu.Unlock()
if r.findErr != nil {
return domain.User{}, r.findErr
}
u, ok := r.users[id]
if !ok {
return domain.User{}, domain.ErrNotFound
}
return u, nil
}

func (r *fakeUserRepo) UpdateProfile(_ context.Context, id domain.ID, name, phone *string, at time.Time) error {
r.mu.Lock()
defer r.mu.Unlock()
r.updateProfileCalls++
u, ok := r.users[id]
if !ok {
return domain.ErrNotFound
}
var err error
if name != nil {
if u, err = u.WithName(*name, at); err != nil {
return err
}
}
if phone != nil {
if u, err = u.WithPhone(*phone, at); err != nil {
return err
}
}
r.users[id] = u
return nil
}

func (r *fakeUserRepo) UpdatePasswordHash(_ context.Context, id domain.ID, hash string, at time.Time) error {
r.mu.Lock()
defer r.mu.Unlock()
r.updatePasswordHashCalls++
u, ok := r.users[id]
if !ok {
return domain.ErrNotFound
}
u2, err := u.WithPasswordHash(hash, at)
if err != nil {
return err
}
r.users[id] = u2
return nil
}

type fakeRefreshRepo struct {
mu            sync.Mutex
byID          map[domain.ID]domain.RefreshToken
byHash        map[string]domain.ID
rotateErr     error
createCalls   int
revokeFamilyC int
}

func newFakeRefreshRepo() *fakeRefreshRepo {
return &fakeRefreshRepo{
byID:   map[domain.ID]domain.RefreshToken{},
byHash: map[string]domain.ID{},
}
}

func (r *fakeRefreshRepo) Create(_ context.Context, t domain.RefreshToken) error {
r.mu.Lock()
defer r.mu.Unlock()
r.createCalls++
r.byID[t.ID()] = t
r.byHash[string(t.TokenHash())] = t.ID()
return nil
}

func (r *fakeRefreshRepo) FindByHash(_ context.Context, hash []byte) (domain.RefreshToken, error) {
r.mu.Lock()
defer r.mu.Unlock()
id, ok := r.byHash[string(hash)]
if !ok {
return domain.RefreshToken{}, domain.ErrNotFound
}
return r.byID[id], nil
}

// Rotate simulates the conditional UPDATE the real adapter must perform:
// if the parent has already been revoked in the store, return ErrConflict.
func (r *fakeRefreshRepo) Rotate(_ context.Context, parent, child domain.RefreshToken) error {
r.mu.Lock()
defer r.mu.Unlock()
if r.rotateErr != nil {
return r.rotateErr
}
existing, ok := r.byID[parent.ID()]
if !ok {
return domain.ErrNotFound
}
if existing.IsRevoked() {
return domain.ErrConflict
}
r.byID[parent.ID()] = parent
r.byID[child.ID()] = child
r.byHash[string(child.TokenHash())] = child.ID()
return nil
}

func (r *fakeRefreshRepo) RevokeFamily(_ context.Context, familyID domain.ID, at time.Time) (int, error) {
r.mu.Lock()
defer r.mu.Unlock()
r.revokeFamilyC++
n := 0
for id, t := range r.byID {
if t.FamilyID() == familyID && !t.IsRevoked() {
r.byID[id] = t.Revoke(at)
n++
}
}
return n, nil
}

func (r *fakeRefreshRepo) ListByUser(_ context.Context, userID domain.ID) ([]domain.RefreshToken, error) {
r.mu.Lock()
defer r.mu.Unlock()
var out []domain.RefreshToken
for _, t := range r.byID {
if t.UserID() == userID {
out = append(out, t)
}
}
return out, nil
}

func (r *fakeRefreshRepo) RevokeByUser(_ context.Context, userID, tokenID domain.ID, at time.Time) error {
r.mu.Lock()
defer r.mu.Unlock()
t, ok := r.byID[tokenID]
if !ok || t.UserID() != userID {
return domain.ErrNotFound
}
r.byID[tokenID] = t.Revoke(at)
return nil
}

func (r *fakeRefreshRepo) seed(t *testing.T, tok domain.RefreshToken) {
t.Helper()
r.mu.Lock()
defer r.mu.Unlock()
r.byID[tok.ID()] = tok
r.byHash[string(tok.TokenHash())] = tok.ID()
}

type fakeAddressRepo struct {
mu     sync.Mutex
byUser map[domain.ID][]domain.Address
}

func newFakeAddressRepo() *fakeAddressRepo {
return &fakeAddressRepo{byUser: map[domain.ID][]domain.Address{}}
}

func (r *fakeAddressRepo) ListByUser(_ context.Context, userID domain.ID) ([]domain.Address, error) {
r.mu.Lock()
defer r.mu.Unlock()
return append([]domain.Address(nil), r.byUser[userID]...), nil
}

func (r *fakeAddressRepo) Create(_ context.Context, a domain.Address) error {
r.mu.Lock()
defer r.mu.Unlock()
r.byUser[a.UserID()] = append(r.byUser[a.UserID()], a)
return nil
}

type fakeAuditLog struct {
mu      sync.Mutex
entries []domain.AuditEntry
err     error
}

func (a *fakeAuditLog) Append(_ context.Context, e domain.AuditEntry) error {
a.mu.Lock()
defer a.mu.Unlock()
if a.err != nil {
return a.err
}
a.entries = append(a.entries, e)
return nil
}

func (a *fakeAuditLog) actions() []string {
a.mu.Lock()
defer a.mu.Unlock()
var out []string
for _, e := range a.entries {
out = append(out, e.Action)
}
return out
}

func (a *fakeAuditLog) findAction(action string) (domain.AuditEntry, bool) {
a.mu.Lock()
defer a.mu.Unlock()
for _, e := range a.entries {
if e.Action == action {
return e, true
}
}
return domain.AuditEntry{}, false
}

type fakeLockout struct {
mu       sync.Mutex
backoffs map[string]time.Duration
resets   []string
fails    []string
}

func newFakeLockout() *fakeLockout {
return &fakeLockout{
backoffs: map[string]time.Duration{},
}
}

func lockoutKey(u domain.ID, prefix string) string {
return u.String() + "|" + prefix
}

func (l *fakeLockout) Backoff(_ context.Context, u domain.ID, prefix string) (time.Duration, error) {
l.mu.Lock()
defer l.mu.Unlock()
return l.backoffs[lockoutKey(u, prefix)], nil
}

func (l *fakeLockout) RecordFailedAttempt(_ context.Context, u domain.ID, prefix string) (time.Duration, error) {
l.mu.Lock()
defer l.mu.Unlock()
l.fails = append(l.fails, lockoutKey(u, prefix))
return 0, nil
}

func (l *fakeLockout) Reset(_ context.Context, u domain.ID, prefix string) error {
l.mu.Lock()
defer l.mu.Unlock()
l.resets = append(l.resets, lockoutKey(u, prefix))
return nil
}

type fakeHasher struct {
prefix      string
verifyCalls int
hashErr     error
verifyErr   error
}

func (h *fakeHasher) Hash(pw string) (string, error) {
if h.hashErr != nil {
return "", h.hashErr
}
return h.prefix + ":" + pw, nil
}

func (h *fakeHasher) Verify(pw, enc string) (bool, bool, error) {
h.verifyCalls++
if h.verifyErr != nil {
return false, false, h.verifyErr
}
i := lastColon(enc)
if i < 0 {
return false, false, nil
}
prefix, stored := enc[:i], enc[i+1:]
if stored != pw {
return false, false, nil
}
return true, prefix != h.prefix, nil
}

func lastColon(s string) int {
for i := len(s) - 1; i >= 0; i-- {
if s[i] == ':' {
return i
}
}
return -1
}

type fakeSigner struct {
err error
}

func (s *fakeSigner) Sign(userID domain.ID, role domain.Role, _ time.Duration) (string, error) {
if s.err != nil {
return "", s.err
}
return fmt.Sprintf("access:%s:%s", userID, role), nil
}

type fakeClock struct {
now time.Time
}

func (c *fakeClock) Now() time.Time {
if c.now.IsZero() {
return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
}
return c.now
}

type fakeIDs struct {
mu sync.Mutex
n  int
}

func (f *fakeIDs) NewID() domain.ID {
f.mu.Lock()
defer f.mu.Unlock()
f.n++
var id domain.ID
id[0] = byte(f.n >> 8)
id[1] = byte(f.n)
id[15] = 1
return id
}

type fakeRaw struct {
mu sync.Mutex
n  int
}

func (r *fakeRaw) NewRawToken() ([]byte, error) {
r.mu.Lock()
defer r.mu.Unlock()
r.n++
return []byte(fmt.Sprintf("raw-%d", r.n)), nil
}

// ---------- harness ----------

type svcDeps struct {
users     *fakeUserRepo
refresh   *fakeRefreshRepo
addresses *fakeAddressRepo
audit     *fakeAuditLog
lockout   *fakeLockout
hasher    *fakeHasher
signer    *fakeSigner
clock     *fakeClock
ids       *fakeIDs
raw       *fakeRaw
}

func newSvc(t *testing.T) (*AuthService, *svcDeps) {
t.Helper()
d := &svcDeps{
users:     newFakeUserRepo(),
refresh:   newFakeRefreshRepo(),
addresses: newFakeAddressRepo(),
audit:     &fakeAuditLog{},
lockout:   newFakeLockout(),
hasher:    &fakeHasher{prefix: "cur"},
signer:    &fakeSigner{},
clock:     &fakeClock{},
ids:       &fakeIDs{},
raw:       &fakeRaw{},
}
svc, err := NewAuthService(
d.users, d.refresh, d.addresses, d.audit, d.lockout,
d.hasher, d.signer, d.clock, d.ids, d.raw,
Config{AccessTTL: 15 * time.Minute, RefreshTTL: 7 * 24 * time.Hour},
)
if err != nil {
t.Fatalf("NewAuthService: %v", err)
}
return svc, d
}

func seedUser(t *testing.T, d *svcDeps, email, password string) domain.User {
t.Helper()
hash, _ := d.hasher.Hash(password)
u, err := domain.NewUser(d.ids.NewID(), email, hash, "Test", domain.RoleCustomer, "", d.clock.Now())
if err != nil {
t.Fatalf("seed user: %v", err)
}
if err := d.users.Create(context.Background(), u); err != nil {
t.Fatalf("seed create: %v", err)
}
return u
}

func seedRefresh(t *testing.T, d *svcDeps, u domain.User, raw []byte) domain.RefreshToken {
t.Helper()
tok, err := domain.NewRefreshToken(
d.ids.NewID(), u.ID(), d.ids.NewID(),
domain.HashRefreshToken(raw),
d.clock.Now().Add(7*24*time.Hour), d.clock.Now(), "ua", "127.0.0.1",
)
if err != nil {
t.Fatalf("seed refresh: %v", err)
}
d.refresh.seed(t, tok)
return tok
}

// ---------- Register ----------

func TestRegister_HappyPath(t *testing.T) {
svc, d := newSvc(t)
u, err := svc.Register(context.Background(), "Alice@Example.COM", "correct-horse", "Alice", "1.2.3.4")
if err != nil {
t.Fatalf("Register: %v", err)
}
if u.Email() != "alice@example.com" {
t.Errorf("Email = %q", u.Email())
}
if u.Role() != domain.RoleCustomer {
t.Errorf("Role = %q", u.Role())
}
if got := d.audit.actions(); len(got) != 1 || got[0] != "user_registered" {
t.Errorf("audit = %v", got)
}
}

func TestRegister_WeakPasswordRejected(t *testing.T) {
svc, _ := newSvc(t)
_, err := svc.Register(context.Background(), "a@b.com", "short", "", "")
if !errors.Is(err, domain.ErrPasswordTooShort) {
t.Fatalf("want ErrPasswordTooShort, got %v", err)
}
}

func TestRegister_DuplicateEmail(t *testing.T) {
svc, d := newSvc(t)
seedUser(t, d, "taken@example.com", "correct-horse")
_, err := svc.Register(context.Background(), "taken@example.com", "correct-horse", "", "")
if !errors.Is(err, ErrEmailTaken) {
t.Fatalf("want ErrEmailTaken, got %v", err)
}
}

func TestRegister_InvalidEmail(t *testing.T) {
svc, _ := newSvc(t)
_, err := svc.Register(context.Background(), "not-an-email", "correct-horse", "", "")
if !errors.Is(err, domain.ErrInvalidEmail) {
t.Fatalf("want ErrInvalidEmail, got %v", err)
}
}

// ---------- Login ----------

func TestLogin_HappyPath(t *testing.T) {
svc, d := newSvc(t)
seedUser(t, d, "user@example.com", "correct-horse")
res, err := svc.Login(context.Background(), "user@example.com", "correct-horse", "1.2.3.4", "ua")
if err != nil {
t.Fatalf("Login: %v", err)
}
if res.AccessToken == "" || len(res.RefreshToken) == 0 {
t.Fatal("tokens not issued")
}
if d.refresh.createCalls != 1 {
t.Errorf("refresh.Create calls = %d", d.refresh.createCalls)
}
if got := d.audit.actions(); len(got) != 1 || got[0] != "login_success" {
t.Errorf("audit = %v", got)
}
}

func TestLogin_WrongPassword(t *testing.T) {
svc, d := newSvc(t)
seedUser(t, d, "user@example.com", "correct-horse")
_, err := svc.Login(context.Background(), "user@example.com", "wrong", "1.2.3.4", "ua")
if !errors.Is(err, ErrInvalidCredentials) {
t.Fatalf("want ErrInvalidCredentials, got %v", err)
}
if len(d.lockout.fails) != 1 {
t.Errorf("lockout failures = %d", len(d.lockout.fails))
}
if got := d.audit.actions(); len(got) != 1 || got[0] != "login_failed" {
t.Errorf("audit = %v", got)
}
}

func TestLogin_UnknownEmailUsesDummyHash(t *testing.T) {
svc, d := newSvc(t)
d.hasher.verifyCalls = 0 // reset after constructor
_, err := svc.Login(context.Background(), "nobody@example.com", "whatever", "1.2.3.4", "ua")
if !errors.Is(err, ErrInvalidCredentials) {
t.Fatalf("want ErrInvalidCredentials, got %v", err)
}
if d.hasher.verifyCalls != 1 {
t.Fatalf("verify calls = %d, want 1 (dummy verify)", d.hasher.verifyCalls)
}
}

func TestLogin_InactiveAccount(t *testing.T) {
svc, d := newSvc(t)
hash, _ := d.hasher.Hash("correct-horse")
u, err := domain.RehydrateUser(domain.UserRecord{
ID: d.ids.NewID(), Email: "user@example.com", PasswordHash: hash,
Role: domain.RoleCustomer, IsActive: false,
CreatedAt: d.clock.Now(), UpdatedAt: d.clock.Now(),
})
if err != nil {
t.Fatalf("rehydrate: %v", err)
}
d.users.users[u.ID()] = u
d.users.byEmail[u.Email()] = u.ID()

if _, err := svc.Login(context.Background(), "user@example.com", "correct-horse", "1.2.3.4", "ua"); !errors.Is(err, ErrInvalidCredentials) {
t.Fatalf("want ErrInvalidCredentials, got %v", err)
}
}

func TestLogin_Locked(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
prefix := ipPrefix(parseIP("1.2.3.4"))
d.lockout.backoffs[lockoutKey(u.ID(), prefix)] = 5 * time.Minute

d.hasher.verifyCalls = 0
_, err := svc.Login(context.Background(), "user@example.com", "correct-horse", "1.2.3.4", "ua")
if !errors.Is(err, ErrInvalidCredentials) {
t.Fatalf("want ErrInvalidCredentials, got %v", err)
}
if d.hasher.verifyCalls != 1 {
t.Errorf("verify calls = %d, want 1 (dummy verify on locked)", d.hasher.verifyCalls)
}
if got := d.audit.actions(); len(got) != 1 || got[0] != "login_locked" {
t.Errorf("audit = %v", got)
}
}

func TestLogin_ResetsLockoutOnSuccess(t *testing.T) {
svc, d := newSvc(t)
seedUser(t, d, "user@example.com", "correct-horse")
if _, err := svc.Login(context.Background(), "user@example.com", "correct-horse", "1.2.3.4", "ua"); err != nil {
t.Fatalf("Login: %v", err)
}
if len(d.lockout.resets) != 1 {
t.Errorf("resets = %d", len(d.lockout.resets))
}
}

func TestLogin_RehashTriggered(t *testing.T) {
svc, d := newSvc(t)
// Seed user with an old-prefix hash.
u, err := domain.NewUser(d.ids.NewID(), "user@example.com", "old:correct-horse", "T", domain.RoleCustomer, "", d.clock.Now())
if err != nil {
t.Fatalf("seed: %v", err)
}
if err := d.users.Create(context.Background(), u); err != nil {
t.Fatalf("seed create: %v", err)
}

if _, err := svc.Login(context.Background(), "user@example.com", "correct-horse", "1.2.3.4", "ua"); err != nil {
t.Fatalf("Login: %v", err)
}
if d.users.updatePasswordHashCalls != 1 {
t.Errorf("UpdatePasswordHash calls = %d", d.users.updatePasswordHashCalls)
}
stored, _ := d.users.FindByID(context.Background(), u.ID())
if stored.PasswordHash() != "cur:correct-horse" {
t.Errorf("hash not rehashed: %q", stored.PasswordHash())
}
}

func TestLogin_SignFailsNoPersist(t *testing.T) {
svc, d := newSvc(t)
seedUser(t, d, "user@example.com", "correct-horse")
d.signer.err = errors.New("boom")
_, err := svc.Login(context.Background(), "user@example.com", "correct-horse", "1.2.3.4", "ua")
if err == nil {
t.Fatal("expected error")
}
if d.refresh.createCalls != 0 {
t.Errorf("refresh.Create calls = %d (must be 0 — sign fails first)", d.refresh.createCalls)
}
}

// ---------- Refresh ----------

func TestRefresh_HappyPath(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
seedRefresh(t, d, u, []byte("raw-1"))

res, err := svc.Refresh(context.Background(), []byte("raw-1"), "1.2.3.4", "ua")
if err != nil {
t.Fatalf("Refresh: %v", err)
}
if res.AccessToken == "" || len(res.RefreshToken) == 0 {
t.Fatal("tokens not issued")
}
}

func TestRefresh_NotFound(t *testing.T) {
svc, _ := newSvc(t)
_, err := svc.Refresh(context.Background(), []byte("does-not-exist"), "1.2.3.4", "ua")
if !errors.Is(err, ErrInvalidCredentials) {
t.Fatalf("want ErrInvalidCredentials, got %v", err)
}
}

func TestRefresh_Expired(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
tok, _ := domain.NewRefreshToken(
d.ids.NewID(), u.ID(), d.ids.NewID(),
domain.HashRefreshToken([]byte("expired")),
d.clock.Now().Add(time.Minute), d.clock.Now(), "ua", "",
)
d.refresh.seed(t, tok)
d.clock.now = d.clock.Now().Add(2 * time.Hour)

_, err := svc.Refresh(context.Background(), []byte("expired"), "1.2.3.4", "ua")
if !errors.Is(err, ErrTokenExpired) {
t.Fatalf("want ErrTokenExpired, got %v", err)
}
if d.refresh.revokeFamilyC != 0 {
t.Errorf("family revoked on plain expiry")
}
}

func TestRefresh_RevokedTriggersFamilyRevoke(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
tok := seedRefresh(t, d, u, []byte("raw-1"))
// Revoke it directly.
revoked := tok.Revoke(d.clock.Now())
d.refresh.byID[tok.ID()] = revoked

_, err := svc.Refresh(context.Background(), []byte("raw-1"), "1.2.3.4", "ua")
if !errors.Is(err, ErrTokenReused) {
t.Fatalf("want ErrTokenReused, got %v", err)
}
if d.refresh.revokeFamilyC == 0 {
t.Error("family not revoked on reuse")
}
if _, ok := d.audit.findAction("refresh_reuse_detected"); !ok {
t.Error("reuse audit missing")
}
}

func TestRefresh_ConcurrentRotateAsReuse(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
seedRefresh(t, d, u, []byte("raw-1"))
d.refresh.rotateErr = domain.ErrConflict

_, err := svc.Refresh(context.Background(), []byte("raw-1"), "1.2.3.4", "ua")
if !errors.Is(err, ErrTokenReused) {
t.Fatalf("want ErrTokenReused, got %v", err)
}
if d.refresh.revokeFamilyC == 0 {
t.Error("family not revoked on concurrent rotate")
}
}

func TestRefresh_SignFailsNoRotate(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
seedRefresh(t, d, u, []byte("raw-1"))
d.signer.err = errors.New("boom")
_, err := svc.Refresh(context.Background(), []byte("raw-1"), "1.2.3.4", "ua")
if err == nil {
t.Fatal("expected error")
}
// Parent must still be unrevoked.
tok, _ := d.refresh.FindByHash(context.Background(), domain.HashRefreshToken([]byte("raw-1")))
if tok.IsRevoked() {
t.Error("parent revoked despite sign failure")
}
}

// ---------- Logout ----------

func TestLogout_RevokesFamily(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
seedRefresh(t, d, u, []byte("raw-1"))
if err := svc.Logout(context.Background(), []byte("raw-1")); err != nil {
t.Fatalf("Logout: %v", err)
}
if d.refresh.revokeFamilyC != 1 {
t.Errorf("RevokeFamily calls = %d", d.refresh.revokeFamilyC)
}
}

func TestLogout_EmptyIsNoop(t *testing.T) {
svc, _ := newSvc(t)
if err := svc.Logout(context.Background(), nil); err != nil {
t.Fatalf("Logout(nil): %v", err)
}
if err := svc.Logout(context.Background(), []byte{}); err != nil {
t.Fatalf("Logout(empty): %v", err)
}
}

func TestLogout_NotFoundIsNoop(t *testing.T) {
svc, _ := newSvc(t)
if err := svc.Logout(context.Background(), []byte("nope")); err != nil {
t.Fatalf("Logout: %v", err)
}
}

// ---------- UpdateMe ----------

func TestUpdateMe_NameOnly(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
name := "Alice"
got, err := svc.UpdateMe(context.Background(), u.ID(), UpdateMePatch{Name: &name})
if err != nil {
t.Fatalf("UpdateMe: %v", err)
}
if got.Name() != "Alice" {
t.Errorf("Name = %q", got.Name())
}
if d.users.updateProfileCalls != 1 {
t.Errorf("UpdateProfile calls = %d", d.users.updateProfileCalls)
}
}

func TestUpdateMe_PhoneOnly(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
phone := "+254712345678"
got, err := svc.UpdateMe(context.Background(), u.ID(), UpdateMePatch{Phone: &phone})
if err != nil {
t.Fatalf("UpdateMe: %v", err)
}
if got.PhoneE164() != "+254712345678" {
t.Errorf("Phone = %q", got.PhoneE164())
}
}

func TestUpdateMe_EmptyPatchRejected(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
if _, err := svc.UpdateMe(context.Background(), u.ID(), UpdateMePatch{}); !errors.Is(err, ErrEmptyPatch) {
t.Fatalf("want ErrEmptyPatch, got %v", err)
}
}

func TestUpdateMe_InvalidNameRejected(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
long := make([]byte, 256)
for i := range long {
long[i] = 'a'
}
name := string(long)
if _, err := svc.UpdateMe(context.Background(), u.ID(), UpdateMePatch{Name: &name}); !errors.Is(err, domain.ErrInvalidName) {
t.Fatalf("want ErrInvalidName, got %v", err)
}
}

func TestUpdateMe_AuditHasNoPII(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
name := "Alice"
phone := "+254712345678"
if _, err := svc.UpdateMe(context.Background(), u.ID(), UpdateMePatch{Name: &name, Phone: &phone}); err != nil {
t.Fatalf("UpdateMe: %v", err)
}
e, ok := d.audit.findAction("user_updated")
if !ok {
t.Fatal("audit missing")
}
// Values must not appear in the audit payload.
if containsString(e.After, "Alice") || containsString(e.After, "+254712345678") {
t.Fatalf("audit leaked PII: %s", e.After)
}
}

func containsString(b []byte, s string) bool {
if len(b) == 0 {
return false
}
target := []byte(s)
for i := 0; i+len(target) <= len(b); i++ {
match := true
for j := range target {
if b[i+j] != target[j] {
match = false
break
}
}
if match {
return true
}
}
return false
}

// ---------- CreateAddress ----------

func TestCreateAddress_HappyPath(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
a, err := svc.CreateAddress(context.Background(), u.ID(), CreateAddressInput{
Recipient: "Bob", Phone: "+254712345678",
Line1: "1 Kimathi", Town: "Nairobi",
})
if err != nil {
t.Fatalf("CreateAddress: %v", err)
}
if a.Recipient() != "Bob" {
t.Errorf("Recipient = %q", a.Recipient())
}
}

func TestCreateAddress_Limit(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
// Prepopulate 10 addresses.
for i := 0; i < 10; i++ {
d.addresses.byUser[u.ID()] = append(d.addresses.byUser[u.ID()], domain.Address{})
}
_, err := svc.CreateAddress(context.Background(), u.ID(), CreateAddressInput{
Recipient: "Bob", Phone: "+254712345678", Line1: "1 K", Town: "Nairobi",
})
if !errors.Is(err, ErrAddressLimit) {
t.Fatalf("want ErrAddressLimit, got %v", err)
}
}

// ---------- ListSessions ----------

func TestListSessions_FiltersInactive(t *testing.T) {
svc, d := newSvc(t)
u := seedUser(t, d, "user@example.com", "correct-horse")
active := seedRefresh(t, d, u, []byte("raw-1"))
revoked := seedRefresh(t, d, u, []byte("raw-2"))
d.refresh.byID[revoked.ID()] = revoked.Revoke(d.clock.Now())

got, err := svc.ListSessions(context.Background(), u.ID())
if err != nil {
t.Fatalf("ListSessions: %v", err)
}
if len(got) != 1 || got[0].ID != active.ID() {
t.Fatalf("got %d sessions, want 1 active", len(got))
}
}

// ---------- IP helpers ----------

func TestIPPrefix_IPv4(t *testing.T) {
ip := netip.MustParseAddr("192.168.1.42")
got := ipPrefix(ip)
if got != "192.168.1.0/24" {
t.Errorf("got %q", got)
}
}

func TestIPPrefix_IPv6(t *testing.T) {
ip := netip.MustParseAddr("2001:db8::1")
got := ipPrefix(ip)
if got != "2001:db8::/64" {
t.Errorf("got %q", got)
}
}

func TestIPPrefix_Invalid(t *testing.T) {
if got := ipPrefix(netip.Addr{}); got != "unknown" {
t.Errorf("got %q, want unknown", got)
}
}
