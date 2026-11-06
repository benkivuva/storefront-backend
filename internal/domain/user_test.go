package domain

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

var userTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func testUserID(t *testing.T) ID {
	t.Helper()
	return mustID(t, sampleID)
}

func mustNewUser(t *testing.T) User {
	t.Helper()
	u, err := NewUser(testUserID(t), "ben@example.com", "hashed", "Ben Kivuva", RoleCustomer, "+254712345678", userTime)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	return u
}

func TestNewUser_Valid(t *testing.T) {
	u := mustNewUser(t)
	if u.ID() != testUserID(t) {
		t.Errorf("ID mismatch")
	}
	if u.Email() != "ben@example.com" {
		t.Errorf("Email = %q", u.Email())
	}
	if u.PasswordHash() != "hashed" {
		t.Errorf("PasswordHash = %q", u.PasswordHash())
	}
	if u.Name() != "Ben Kivuva" {
		t.Errorf("Name = %q", u.Name())
	}
	if u.Role() != RoleCustomer {
		t.Errorf("Role = %q", u.Role())
	}
	if u.PhoneE164() != "+254712345678" {
		t.Errorf("PhoneE164 = %q", u.PhoneE164())
	}
	if !u.IsActive() {
		t.Errorf("new user must be active")
	}
	if u.IsEmailVerified() {
		t.Errorf("new user must not be verified")
	}
	if !u.CreatedAt().Equal(userTime) || !u.UpdatedAt().Equal(userTime) {
		t.Errorf("timestamps mismatch: %v / %v", u.CreatedAt(), u.UpdatedAt())
	}
}

func TestNewUser_EmptyNameAllowed(t *testing.T) {
	u, err := NewUser(testUserID(t), "b@e.com", "h", "", RoleCustomer, "", userTime)
	if err != nil {
		t.Fatalf("empty name must be allowed: %v", err)
	}
	if u.Name() != "" {
		t.Errorf("Name = %q, want empty", u.Name())
	}
}

func TestNewUser_NormalizesName(t *testing.T) {
	u, err := NewUser(testUserID(t), "b@e.com", "h", "  Ben Kivuva  ", RoleCustomer, "", userTime)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if u.Name() != "Ben Kivuva" {
		t.Errorf("Name = %q", u.Name())
	}
}

func TestNewUser_RejectsOversizedName(t *testing.T) {
	long := strings.Repeat("a", 256)
	_, err := NewUser(testUserID(t), "b@e.com", "h", long, RoleCustomer, "", userTime)
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("want ErrInvalidName, got %v", err)
	}
}

func TestNewUser_RejectsZeroID(t *testing.T) {
	_, err := NewUser(ID{}, "b@e.com", "h", "", RoleCustomer, "", userTime)
	if !errors.Is(err, ErrZeroUserID) {
		t.Fatalf("want ErrZeroUserID, got %v", err)
	}
}

func TestNewUser_RejectsInvalidEmail(t *testing.T) {
	cases := []string{
		"",
		"not-an-email",
		"ben@",
		"@example.com",
		"ben@example",
		"ben@example .com",
		"<ben@example.com>",
		strings.Repeat("a", 250) + "@example.com",
	}
	for _, e := range cases {
		t.Run(e, func(t *testing.T) {
			_, err := NewUser(testUserID(t), e, "h", "", RoleCustomer, "", userTime)
			if !errors.Is(err, ErrInvalidEmail) {
				t.Fatalf("email=%q: want ErrInvalidEmail, got %v", e, err)
			}
		})
	}
}

func TestNewUser_NormalizesEmail(t *testing.T) {
	for in, want := range map[string]string{
		"BEN@Example.COM":     "ben@example.com",
		"  ben@example.com  ": "ben@example.com",
		"Ben+tag@Example.com": "ben+tag@example.com",
	} {
		u, err := NewUser(testUserID(t), in, "h", "", RoleCustomer, "", userTime)
		if err != nil {
			t.Fatalf("email=%q: %v", in, err)
		}
		if u.Email() != want {
			t.Errorf("email=%q normalized to %q, want %q", in, u.Email(), want)
		}
	}
}

func TestNewUser_RejectsEmptyPasswordHash(t *testing.T) {
	_, err := NewUser(testUserID(t), "b@e.com", "", "", RoleCustomer, "", userTime)
	if !errors.Is(err, ErrEmptyPasswordHash) {
		t.Fatalf("want ErrEmptyPasswordHash, got %v", err)
	}
}

func TestNewUser_RejectsInvalidRole(t *testing.T) {
		for _, r := range []Role{"", "customer ", "Customer", "root", "ROOT", "ADMIN", "Admin"} {
		_, err := NewUser(testUserID(t), "b@e.com", "h", "", r, "", userTime)
		if !errors.Is(err, ErrInvalidRole) {
			t.Errorf("role=%q: want ErrInvalidRole, got %v", r, err)
		}
	}
}

func TestNewUser_EmptyPhoneAllowed(t *testing.T) {
	u, err := NewUser(testUserID(t), "b@e.com", "h", "", RoleCustomer, "", userTime)
	if err != nil {
		t.Fatalf("empty phone must be allowed: %v", err)
	}
	if u.PhoneE164() != "" {
		t.Errorf("PhoneE164 = %q, want empty", u.PhoneE164())
	}
}

func TestNewUser_RejectsInvalidPhone(t *testing.T) {
	cases := []string{
		"+25471234567",
		"+2547123456789",
		"254712345678",
		"+254812345678",
		"+254212345678",
		"+25471234abcd",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			_, err := NewUser(testUserID(t), "b@e.com", "h", "", RoleCustomer, p, userTime)
			if !errors.Is(err, ErrInvalidPhone) {
				t.Fatalf("phone=%q: want ErrInvalidPhone, got %v", p, err)
			}
		})
	}
}

func TestNewUser_NormalizesPhone(t *testing.T) {
	u, err := NewUser(testUserID(t), "b@e.com", "h", "", RoleCustomer, "  +254712345678  ", userTime)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if u.PhoneE164() != "+254712345678" {
		t.Errorf("PhoneE164 = %q", u.PhoneE164())
	}
}

func TestNewUser_RejectsZeroTime(t *testing.T) {
	_, err := NewUser(testUserID(t), "b@e.com", "h", "", RoleCustomer, "", time.Time{})
	if !errors.Is(err, ErrZeroUserTime) {
		t.Fatalf("want ErrZeroUserTime, got %v", err)
	}
}

func TestWithName(t *testing.T) {
	u := mustNewUser(t)
	at := userTime.Add(time.Hour)

	updated, err := u.WithName("New Name", at)
	if err != nil {
		t.Fatalf("WithName: %v", err)
	}
	if updated.Name() != "New Name" {
		t.Errorf("Name = %q", updated.Name())
	}
	if !updated.UpdatedAt().Equal(at) {
		t.Errorf("UpdatedAt not advanced")
	}
	if u.Name() != "Ben Kivuva" {
		t.Errorf("original user was mutated")
	}

	cleared, err := u.WithName("", at)
	if err != nil || cleared.Name() != "" {
		t.Errorf("clearing failed: %v %q", err, cleared.Name())
	}

	if _, err := u.WithName(strings.Repeat("x", 256), at); !errors.Is(err, ErrInvalidName) {
		t.Errorf("want ErrInvalidName, got %v", err)
	}
	if _, err := u.WithName("ok", time.Time{}); !errors.Is(err, ErrZeroUserTime) {
		t.Errorf("want ErrZeroUserTime, got %v", err)
	}
}

func TestWithPhone(t *testing.T) {
	u := mustNewUser(t)
	at := userTime.Add(time.Hour)

	updated, err := u.WithPhone("+254112345678", at)
	if err != nil {
		t.Fatalf("WithPhone: %v", err)
	}
	if updated.PhoneE164() != "+254112345678" {
		t.Errorf("PhoneE164 = %q", updated.PhoneE164())
	}
	if !updated.UpdatedAt().Equal(at) {
		t.Errorf("UpdatedAt not advanced")
	}
	if u.PhoneE164() != "+254712345678" {
		t.Errorf("original user was mutated")
	}

	cleared, err := u.WithPhone("", at)
	if err != nil || cleared.PhoneE164() != "" {
		t.Errorf("clearing failed: %v %q", err, cleared.PhoneE164())
	}

	if _, err := u.WithPhone("bogus", at); !errors.Is(err, ErrInvalidPhone) {
		t.Errorf("want ErrInvalidPhone, got %v", err)
	}
	if _, err := u.WithPhone("+254712345678", time.Time{}); !errors.Is(err, ErrZeroUserTime) {
		t.Errorf("want ErrZeroUserTime, got %v", err)
	}
}

func TestWithPasswordHash(t *testing.T) {
	u := mustNewUser(t)
	at := userTime.Add(time.Hour)

	updated, err := u.WithPasswordHash("new-hash", at)
	if err != nil {
		t.Fatalf("WithPasswordHash: %v", err)
	}
	if updated.PasswordHash() != "new-hash" {
		t.Errorf("PasswordHash = %q", updated.PasswordHash())
	}
	if !updated.UpdatedAt().Equal(at) {
		t.Errorf("UpdatedAt not advanced")
	}
	if u.PasswordHash() != "hashed" {
		t.Errorf("original user was mutated")
	}

	if _, err := u.WithPasswordHash("", at); !errors.Is(err, ErrEmptyPasswordHash) {
		t.Errorf("want ErrEmptyPasswordHash, got %v", err)
	}
	if _, err := u.WithPasswordHash("h", time.Time{}); !errors.Is(err, ErrZeroUserTime) {
		t.Errorf("want ErrZeroUserTime, got %v", err)
	}
}

func TestRole_IsValid(t *testing.T) {
	if !RoleCustomer.IsValid() || !RoleAdmin.IsValid() {
		t.Fatal("known roles must be valid")
	}
	for _, r := range []Role{"", "customer ", "Customer", "root", "admin\n"} {
		if r.IsValid() {
			t.Errorf("role %q should be invalid", r)
		}
	}
}

func TestRehydrateUser_RoundTrip(t *testing.T) {
	orig := mustNewUser(t)
	got, err := RehydrateUser(UserRecord{
		ID:              orig.ID(),
		Email:           orig.Email(),
		PasswordHash:    orig.PasswordHash(),
		Name:            orig.Name(),
		Role:            orig.Role(),
		PhoneE164:       orig.PhoneE164(),
		IsActive:        orig.IsActive(),
		EmailVerifiedAt: orig.EmailVerifiedAt(),
		CreatedAt:       orig.CreatedAt(),
		UpdatedAt:       orig.UpdatedAt(),
	})
	if err != nil {
		t.Fatalf("RehydrateUser: %v", err)
	}
	if orig != got {
		t.Fatalf("round trip changed user:\n orig=%+v\n got=%+v", orig, got)
	}
}

func TestRehydrateUser_PreservesInactiveAndVerified(t *testing.T) {
	verifiedAt := userTime.Add(48 * time.Hour)
	u, err := RehydrateUser(UserRecord{
		ID:              testUserID(t),
		Email:           "b@e.com",
		PasswordHash:    "h",
		Name:            "Admin",
		Role:            RoleAdmin,
		PhoneE164:       "+254712345678",
		IsActive:        false,
		EmailVerifiedAt: verifiedAt,
		CreatedAt:       userTime,
		UpdatedAt:       userTime,
	})
	if err != nil {
		t.Fatalf("RehydrateUser: %v", err)
	}
	if u.IsActive() {
		t.Errorf("inactive flag lost")
	}
	if !u.IsEmailVerified() || !u.EmailVerifiedAt().Equal(verifiedAt) {
		t.Errorf("email_verified_at lost")
	}
	if u.Role() != RoleAdmin {
		t.Errorf("Role = %q", u.Role())
	}
	if u.Name() != "Admin" {
		t.Errorf("Name = %q", u.Name())
	}
}

func TestProperty_NewUserRehydrateRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(61, 62))
	for i := 0; i < 200; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		hash := fmt.Sprintf("hash-%d", i)
		name := ""
		if r.IntN(2) == 0 {
			name = fmt.Sprintf("Name %d", i)
		}
		phone := ""
		if r.IntN(2) == 0 {
			phone = "+254712345678"
		}
		at := userTime.Add(time.Duration(r.Int64N(3600)) * time.Second)

		u1, err := NewUser(testUserID(t), email, hash, name, RoleCustomer, phone, at)
		if err != nil {
			t.Fatalf("iter %d: NewUser: %v", i, err)
		}
		u2, err := RehydrateUser(UserRecord{
			ID:              u1.ID(),
			Email:           u1.Email(),
			PasswordHash:    u1.PasswordHash(),
			Name:            u1.Name(),
			Role:            u1.Role(),
			PhoneE164:       u1.PhoneE164(),
			IsActive:        u1.IsActive(),
			EmailVerifiedAt: u1.EmailVerifiedAt(),
			CreatedAt:       u1.CreatedAt(),
			UpdatedAt:       u1.UpdatedAt(),
		})
		if err != nil {
			t.Fatalf("iter %d: RehydrateUser: %v", i, err)
		}
		if u1 != u2 {
			t.Fatalf("iter %d: round trip differs", i)
		}
	}
}

func TestProperty_EmailNormalizationIdempotent(t *testing.T) {
	for i := 0; i < 100; i++ {
		raw := fmt.Sprintf("User%d@Example.COM", i)
		u1, err := NewUser(testUserID(t), raw, "h", "", RoleCustomer, "", userTime)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		u2, err := NewUser(testUserID(t), u1.Email(), "h", "", RoleCustomer, "", userTime)
		if err != nil {
			t.Fatalf("iter %d: second NewUser: %v", i, err)
		}
		if u1.Email() != u2.Email() {
			t.Fatalf("iter %d: %q != %q", i, u1.Email(), u2.Email())
		}
	}
}