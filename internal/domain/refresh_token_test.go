package domain

import (
"bytes"
"errors"
"testing"
"time"
)

var (
rtNow       = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
rtExpiry    = rtNow.Add(7 * 24 * time.Hour)
rtRawToken  = []byte("raw-token-bytes-opaque-to-caller")
)

func testRefreshHash() []byte { return HashRefreshToken(rtRawToken) }

func testRefreshIDs(t *testing.T) (id, userID, familyID ID) {
t.Helper()
return mustID(t, sampleID), mustID(t, otherID), mustID(t, "11111111-2222-7333-8444-555555555555")
}

func mustNewRefreshToken(t *testing.T) RefreshToken {
t.Helper()
id, userID, familyID := testRefreshIDs(t)
tok, err := NewRefreshToken(id, userID, familyID, testRefreshHash(), rtExpiry, rtNow, "curl/8", "127.0.0.1")
if err != nil {
t.Fatalf("NewRefreshToken: %v", err)
}
return tok
}

func TestNewRefreshToken_Valid(t *testing.T) {
tok := mustNewRefreshToken(t)
id, userID, familyID := testRefreshIDs(t)

if tok.ID() != id || tok.UserID() != userID || tok.FamilyID() != familyID {
t.Errorf("identity fields mismatch")
}
if !bytes.Equal(tok.TokenHash(), testRefreshHash()) {
t.Errorf("TokenHash mismatch")
}
if !tok.ExpiresAt().Equal(rtExpiry) || !tok.CreatedAt().Equal(rtNow) {
t.Errorf("timestamps mismatch")
}
if tok.IsRevoked() {
t.Errorf("fresh token must not be revoked")
}
if !tok.IsActive(rtNow) {
t.Errorf("fresh token must be active")
}
}

func TestNewRefreshToken_RejectsBadHashLength(t *testing.T) {
id, userID, familyID := testRefreshIDs(t)
for _, h := range [][]byte{nil, {}, make([]byte, 16), make([]byte, 64)} {
_, err := NewRefreshToken(id, userID, familyID, h, rtExpiry, rtNow, "", "")
if !errors.Is(err, ErrEmptyRefreshTokenHash) {
t.Errorf("hash len %d: want ErrEmptyRefreshTokenHash, got %v", len(h), err)
}
}
}

func TestNewRefreshToken_RejectsExpiryBeforeCreation(t *testing.T) {
id, userID, familyID := testRefreshIDs(t)
_, err := NewRefreshToken(id, userID, familyID, testRefreshHash(),
rtNow.Add(-time.Second), rtNow, "", "")
if !errors.Is(err, ErrZeroRefreshTokenTime) {
t.Fatalf("want ErrZeroRefreshTokenTime, got %v", err)
}
}

func TestNewRefreshToken_RejectsZeroFields(t *testing.T) {
_, u, f := testRefreshIDs(t)
cases := []struct {
name string
fn   func() error
want error
}{
{"zero id", func() error { _, e := NewRefreshToken(ID{}, u, f, testRefreshHash(), rtExpiry, rtNow, "", ""); return e }, ErrZeroRefreshTokenID},
{"zero user", func() error { _, e := NewRefreshToken(mustID(t, sampleID), ID{}, f, testRefreshHash(), rtExpiry, rtNow, "", ""); return e }, ErrZeroRefreshTokenUserID},
{"zero family", func() error { _, e := NewRefreshToken(mustID(t, sampleID), u, ID{}, testRefreshHash(), rtExpiry, rtNow, "", ""); return e }, ErrZeroFamilyID},
{"zero created", func() error { _, e := NewRefreshToken(mustID(t, sampleID), u, f, testRefreshHash(), rtExpiry, time.Time{}, "", ""); return e }, ErrZeroRefreshTokenTime},
}
for _, tc := range cases {
t.Run(tc.name, func(t *testing.T) {
if err := tc.fn(); !errors.Is(err, tc.want) {
t.Fatalf("want %v, got %v", tc.want, err)
}
})
}
}

func TestRefreshToken_TokenHashReturnsCopy(t *testing.T) {
tok := mustNewRefreshToken(t)
h := tok.TokenHash()
h[0] ^= 0xff
if bytes.Equal(h, tok.TokenHash()) {
t.Fatal("TokenHash returned shared backing array")
}
}

func TestRotate_ProducesChildInSameFamily(t *testing.T) {
parent := mustNewRefreshToken(t)
newID := mustID(t, "22222222-3333-7444-8555-666666666666")
newHash := HashRefreshToken([]byte("child-raw"))
at := rtNow.Add(time.Hour)
newExpiry := rtExpiry.Add(time.Hour)

parentAfter, child, err := parent.Rotate(newID, newHash, newExpiry, at)
if err != nil {
t.Fatalf("Rotate: %v", err)
}

if !parentAfter.IsRevoked() {
t.Error("parent not revoked")
}
if !parentAfter.RevokedAt().Equal(at) {
t.Errorf("parent RevokedAt = %v, want %v", parentAfter.RevokedAt(), at)
}
if parentAfter.ReplacedBy() != newID {
t.Errorf("parent ReplacedBy = %v, want %v", parentAfter.ReplacedBy(), newID)
}

if child.FamilyID() != parent.FamilyID() {
t.Error("child family changed")
}
if child.UserID() != parent.UserID() {
t.Error("child user changed")
}
if child.IsRevoked() {
t.Error("child must not be revoked")
}
if !child.IsActive(at) {
t.Error("child must be active")
}

if parent.IsRevoked() {
t.Error("original parent was mutated (should be immutable)")
}
}

func TestRotate_RejectsAlreadyRevoked(t *testing.T) {
tok := mustNewRefreshToken(t).Revoke(rtNow.Add(time.Minute))
_, _, err := tok.Rotate(mustID(t, "22222222-3333-7444-8555-666666666666"),
HashRefreshToken([]byte("x")), rtExpiry, rtNow)
if !errors.Is(err, ErrRefreshTokenAlreadyRev) {
t.Fatalf("want ErrRefreshTokenAlreadyRev, got %v", err)
}
}

func TestRotate_RejectsSelfReplacement(t *testing.T) {
tok := mustNewRefreshToken(t)
_, _, err := tok.Rotate(tok.ID(), HashRefreshToken([]byte("x")), rtExpiry, rtNow)
if !errors.Is(err, ErrRefreshTokenRotationSelf) {
t.Fatalf("want ErrRefreshTokenRotationSelf, got %v", err)
}
}

func TestRevoke_Idempotent(t *testing.T) {
tok := mustNewRefreshToken(t)
first := tok.Revoke(rtNow.Add(time.Minute))
second := first.Revoke(rtNow.Add(2 * time.Minute))
if !first.RevokedAt().Equal(second.RevokedAt()) {
t.Fatalf("Revoke mutated the already-revoked timestamp: %v -> %v",
first.RevokedAt(), second.RevokedAt())
}
}

func TestIsExpired(t *testing.T) {
tok := mustNewRefreshToken(t)
if tok.IsExpired(rtNow) {
t.Fatal("just-created token marked expired")
}
if !tok.IsExpired(rtExpiry) {
t.Fatal("token at exact expiry must be expired")
}
if !tok.IsExpired(rtExpiry.Add(time.Second)) {
t.Fatal("past expiry must be expired")
}
}

func TestIsActive(t *testing.T) {
tok := mustNewRefreshToken(t)
if !tok.IsActive(rtNow) {
t.Fatal("fresh token must be active")
}
if tok.IsActive(rtExpiry.Add(time.Second)) {
t.Fatal("expired token must not be active")
}
revoked := tok.Revoke(rtNow.Add(time.Minute))
if revoked.IsActive(rtNow.Add(2 * time.Minute)) {
t.Fatal("revoked token must not be active")
}
}

func TestRehydrateRefreshToken_RoundTrip(t *testing.T) {
orig := mustNewRefreshToken(t)
revoked := orig.Revoke(rtNow.Add(time.Minute))

got, err := RehydrateRefreshToken(RefreshTokenRecord{
ID:         revoked.ID(),
UserID:     revoked.UserID(),
FamilyID:   revoked.FamilyID(),
TokenHash:  revoked.TokenHash(),
ExpiresAt:  revoked.ExpiresAt(),
RevokedAt:  revoked.RevokedAt(),
ReplacedBy: revoked.ReplacedBy(),
UserAgent:  revoked.UserAgent(),
IP:         revoked.IP(),
CreatedAt:  revoked.CreatedAt(),
})
if err != nil {
t.Fatalf("Rehydrate: %v", err)
}
if !got.IsRevoked() || !got.RevokedAt().Equal(revoked.RevokedAt()) {
t.Fatal("revocation state lost")
}
if !bytes.Equal(got.TokenHash(), revoked.TokenHash()) {
t.Fatal("token hash lost")
}
}

func TestHashRefreshToken_Deterministic(t *testing.T) {
raw := []byte("some-token-value")
h1 := HashRefreshToken(raw)
h2 := HashRefreshToken(raw)
if !bytes.Equal(h1, h2) {
t.Fatal("hash not deterministic")
}
if len(h1) != 32 {
t.Fatalf("hash length = %d, want 32", len(h1))
}
}

func TestHashRefreshToken_DifferentInputsDifferentHashes(t *testing.T) {
h1 := HashRefreshToken([]byte("token-a"))
h2 := HashRefreshToken([]byte("token-b"))
if bytes.Equal(h1, h2) {
t.Fatal("different inputs produced same hash")
}
}
