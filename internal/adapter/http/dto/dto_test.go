package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

var testID = domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

func testUser(t *testing.T) domain.User {
	t.Helper()
	u, err := domain.NewUser(
		testID, "a@b.com", "hash", "Alice",
		domain.RoleCustomer, "+254712345678",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	return u
}

func TestFromUser(t *testing.T) {
	got := FromUser(testUser(t))
	if got.ID != testID.String() {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Email != "a@b.com" {
		t.Errorf("Email = %q", got.Email)
	}
	if got.Name != "Alice" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Role != "customer" {
		t.Errorf("Role = %q", got.Role)
	}
	if got.Phone != "+254712345678" {
		t.Errorf("Phone = %q", got.Phone)
	}
	if got.IsEmailVerified {
		t.Error("fresh user must not be verified")
	}
}

func TestFromUser_JSONShape(t *testing.T) {
	body, err := json.Marshal(FromUser(testUser(t)))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"id", "email", "name", "role", "is_email_verified", "created_at"} {
		if _, ok := p[k]; !ok {
			t.Errorf("missing field %q", k)
		}
	}
	if _, ok := p["password_hash"]; ok {
		t.Error("password_hash leaked into wire format")
	}
}

func TestFromUser_OmitsEmptyOptionals(t *testing.T) {
	u, _ := domain.NewUser(
		testID, "a@b.com", "hash", "", domain.RoleCustomer, "",
		time.Now().UTC(),
	)
	body, _ := json.Marshal(FromUser(u))
	var p map[string]any
	_ = json.Unmarshal(body, &p)
	if _, ok := p["phone"]; ok {
		t.Error("empty phone should be omitted")
	}
	if _, ok := p["name"]; ok {
		t.Error("empty name should be omitted")
	}
}

func TestFromAddress(t *testing.T) {
	a, err := domain.NewAddress(
		testID, testID,
		"Jane", "+254712345678",
		"12 Kimathi", "", "Nairobi", "Nairobi", "",
		time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	got := FromAddress(a)
	if got.Recipient != "Jane" || got.Town != "Nairobi" || got.Line1 != "12 Kimathi" {
		t.Errorf("mismatch: %+v", got)
	}
}

func TestFromAddresses_Nil(t *testing.T) {
	got := FromAddresses(nil)
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestFromSession(t *testing.T) {
	now := time.Now().UTC()
	s := service.SessionInfo{
		ID:        testID,
		UserAgent: "test-agent",
		IP:        "1.2.3.4",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	got := FromSession(s)
	if got.ID != testID.String() || got.UserAgent != "test-agent" || got.IP != "1.2.3.4" {
		t.Errorf("mismatch: %+v", got)
	}
	body, _ := json.Marshal(got)
	var p map[string]any
	_ = json.Unmarshal(body, &p)
	if _, ok := p["token_hash"]; ok {
		t.Error("token_hash leaked into Session DTO")
	}
}

func TestFromSessions_Nil(t *testing.T) {
	got := FromSessions(nil)
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
