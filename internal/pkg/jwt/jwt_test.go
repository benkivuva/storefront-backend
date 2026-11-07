package jwt

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"
)

const (
	testKID      = "kid-2026-01"
	testIssuer   = "storefront-backend"
	testAudience = "storefront"
)

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func testKeySet(t *testing.T) *KeySet {
	t.Helper()
	_, priv := testKeyPair(t)
	ks, err := NewKeySet(testKID, priv, testIssuer, testAudience, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

func TestSignVerify_RoundTrip(t *testing.T) {
	ks := testKeySet(t)
	ttl := 15 * time.Minute
	raw, err := ks.Sign("user-123", "customer", ttl)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	tok, err := ks.Verify(raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tok.UserID != "user-123" {
		t.Errorf("UserID = %q", tok.UserID)
	}
	if tok.Role != "customer" {
		t.Errorf("Role = %q", tok.Role)
	}
	if tok.JTI == "" {
		t.Errorf("JTI empty")
	}
	if tok.ExpiresAt.Sub(tok.IssuedAt) != ttl {
		t.Errorf("exp-iat = %v, want %v", tok.ExpiresAt.Sub(tok.IssuedAt), ttl)
	}
}

func TestSign_UniqueJTI(t *testing.T) {
	ks := testKeySet(t)
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		raw, err := ks.Sign("user-1", "customer", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		tok, err := ks.Verify(raw)
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok.JTI] {
			t.Fatalf("iter %d: duplicate JTI %q", i, tok.JTI)
		}
		seen[tok.JTI] = true
	}
}

func TestSign_Validation(t *testing.T) {
	ks := testKeySet(t)
	cases := []struct {
		name         string
		userID, role string
		ttl          time.Duration
		wantErr      error
	}{
		{"empty user", "", "customer", time.Hour, ErrEmptySubject},
		{"empty role", "user-1", "", time.Hour, ErrEmptyRole},
		{"zero ttl", "user-1", "customer", 0, ErrInvalidTTL},
		{"negative ttl", "user-1", "customer", -time.Minute, ErrInvalidTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ks.Sign(tc.userID, tc.role, tc.ttl)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestNewKeySet_Validation(t *testing.T) {
	_, priv := testKeyPair(t)
	cases := []struct {
		name     string
		kid      string
		priv     ed25519.PrivateKey
		issuer   string
		audience string
		wantErr  error
	}{
		{"empty kid", "", priv, "iss", "aud", ErrEmptySignKID},
		{"bad priv len", "kid", []byte{1, 2, 3}, "iss", "aud", ErrInvalidPrivKey},
		{"empty issuer", "kid", priv, "", "aud", ErrEmptyIssuer},
		{"empty audience", "kid", priv, "iss", "", ErrEmptyAudience},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewKeySet(tc.kid, tc.priv, tc.issuer, tc.audience, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// Rejection tests forge a token manually to bypass Sign's normal path.

func forgeToken(t *testing.T, kid string, alg golangjwt.SigningMethod, signer interface{}, mutators ...func(*golangjwt.RegisteredClaims)) string {
	t.Helper()
	rc := golangjwt.RegisteredClaims{
		Issuer:    testIssuer,
		Subject:   "user-1",
		Audience:  []string{testAudience},
		ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  golangjwt.NewNumericDate(time.Now()),
		ID:        "jti",
	}
	for _, m := range mutators {
		m(&rc)
	}
	c := claims{Role: "customer", RegisteredClaims: rc}
	tok := golangjwt.NewWithClaims(alg, c)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	raw, err := tok.SignedString(signer)
	if err != nil {
		t.Fatalf("forge: %v", err)
	}
	return raw
}

func TestVerify_RejectsMissingKID(t *testing.T) {
	ks := testKeySet(t)
	raw := forgeToken(t, "", golangjwt.SigningMethodEdDSA, ks.signPriv)
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("token without kid accepted")
	}
}

func TestVerify_RejectsUnknownKID(t *testing.T) {
	ks := testKeySet(t)
	raw := forgeToken(t, "kid-does-not-exist", golangjwt.SigningMethodEdDSA, ks.signPriv)
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("token with unknown kid accepted")
	}
}

func TestVerify_RejectsAlgNone(t *testing.T) {
	ks := testKeySet(t)
	raw := forgeToken(t, testKID, golangjwt.SigningMethodNone, golangjwt.UnsafeAllowNoneSignatureType)
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("alg=none token accepted")
	}
}

func TestVerify_RejectsAlgHS256(t *testing.T) {
	ks := testKeySet(t)
	raw := forgeToken(t, testKID, golangjwt.SigningMethodHS256, []byte("attacker-secret"))
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("alg=HS256 token accepted")
	}
}

// Classic algorithm confusion: sign with HS256 using the EdDSA public key
// bytes as the HMAC secret. Without WithValidMethods, HMAC verification
// would use the same bytes and the forged signature would match.
func TestVerify_RejectsAlgConfusion(t *testing.T) {
	ks := testKeySet(t)
	pub := ks.verifyMap[testKID]
	raw := forgeToken(t, testKID, golangjwt.SigningMethodHS256, []byte(pub))
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("algorithm confusion attack accepted")
	}
}

func TestVerify_RejectsExpired(t *testing.T) {
	ks := testKeySet(t)
	raw := forgeToken(t, testKID, golangjwt.SigningMethodEdDSA, ks.signPriv,
		func(rc *golangjwt.RegisteredClaims) {
			rc.ExpiresAt = golangjwt.NewNumericDate(time.Now().Add(-time.Hour))
			rc.IssuedAt = golangjwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
		})
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestVerify_RejectsWrongIssuer(t *testing.T) {
	ks := testKeySet(t)
	raw := forgeToken(t, testKID, golangjwt.SigningMethodEdDSA, ks.signPriv,
		func(rc *golangjwt.RegisteredClaims) {
			rc.Issuer = "someone-else"
		})
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("wrong issuer accepted")
	}
}

func TestVerify_RejectsWrongAudience(t *testing.T) {
	ks := testKeySet(t)
	raw := forgeToken(t, testKID, golangjwt.SigningMethodEdDSA, ks.signPriv,
		func(rc *golangjwt.RegisteredClaims) {
			rc.Audience = []string{"some-other-service"}
		})
	if _, err := ks.Verify(raw); err == nil {
		t.Fatal("wrong audience accepted")
	}
}

func TestVerify_RejectsTamperedPayload(t *testing.T) {
	ks := testKeySet(t)
	raw, err := ks.Sign("user-1", "customer", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Flip one byte in the payload section
	i := len(raw) / 2
	b := []byte(raw)
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	if _, err := ks.Verify(string(b)); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestVerify_RejectsMalformed(t *testing.T) {
	ks := testKeySet(t)
	cases := []string{"", "not.a.jwt", "a.b", "a.b.c.d"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if _, err := ks.Verify(raw); err == nil {
				t.Fatalf("malformed %q accepted", raw)
			}
		})
	}
}

func TestVerify_PreviousKeyStillWorks(t *testing.T) {
	oldPub, oldPriv := testKeyPair(t)
	oldKS, err := NewKeySet("kid-old", oldPriv, testIssuer, testAudience, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldToken, err := oldKS.Sign("user-1", "customer", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	_, newPriv := testKeyPair(t)
	newKS, err := NewKeySet("kid-new", newPriv, testIssuer, testAudience,
		[]PreviousKey{{KID: "kid-old", Public: oldPub}})
	if err != nil {
		t.Fatal(err)
	}

	tok, err := newKS.Verify(oldToken)
	if err != nil {
		t.Fatalf("old token rejected after rotation: %v", err)
	}
	if tok.UserID != "user-1" {
		t.Errorf("UserID = %q", tok.UserID)
	}

	// New tokens sign with new kid, not old.
	newToken, err := newKS.Sign("user-2", "customer", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := golangjwt.NewParser().ParseUnverified(newToken, &claims{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header["kid"] != "kid-new" {
		t.Errorf("new token kid = %v, want kid-new", parsed.Header["kid"])
	}
}
