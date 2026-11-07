package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testAddressID(t *testing.T) ID {
	t.Helper()
	return mustID(t, otherID)
}

func testAddressTime() time.Time {
	return time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)
}

func mustNewAddress(t *testing.T) Address {
	t.Helper()
	a, err := NewAddress(
		testAddressID(t), testUserID(t),
		"Jane Wanjiku", "+254712345678",
		"12 Kimathi Street", "Apt 4B", "Nairobi", "Nairobi",
		"Ring the bell twice",
		testAddressTime(),
	)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	return a
}

func TestNewAddress_Valid(t *testing.T) {
	a := mustNewAddress(t)
	if a.ID() != testAddressID(t) {
		t.Errorf("ID mismatch")
	}
	if a.UserID() != testUserID(t) {
		t.Errorf("UserID mismatch")
	}
	if a.Recipient() != "Jane Wanjiku" {
		t.Errorf("Recipient = %q", a.Recipient())
	}
	if a.PhoneE164() != "+254712345678" {
		t.Errorf("PhoneE164 = %q", a.PhoneE164())
	}
	if a.Line1() != "12 Kimathi Street" {
		t.Errorf("Line1 = %q", a.Line1())
	}
	if a.Line2() != "Apt 4B" {
		t.Errorf("Line2 = %q", a.Line2())
	}
	if a.Town() != "Nairobi" {
		t.Errorf("Town = %q", a.Town())
	}
	if a.County() != "Nairobi" {
		t.Errorf("County = %q", a.County())
	}
	if a.Notes() != "Ring the bell twice" {
		t.Errorf("Notes = %q", a.Notes())
	}
	if !a.CreatedAt().Equal(testAddressTime()) {
		t.Errorf("CreatedAt = %v", a.CreatedAt())
	}
}

func TestNewAddress_OptionalFieldsEmpty(t *testing.T) {
	a, err := NewAddress(
		testAddressID(t), testUserID(t),
		"Jane Wanjiku", "+254712345678",
		"12 Kimathi Street", "", "Nairobi", "", "",
		testAddressTime(),
	)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	if a.Line2() != "" || a.County() != "" || a.Notes() != "" {
		t.Errorf("optional fields should be empty")
	}
}

func TestNewAddress_TrimsWhitespace(t *testing.T) {
	a, err := NewAddress(
		testAddressID(t), testUserID(t),
		"  Jane Wanjiku  ", "  +254712345678  ",
		"  12 Kimathi Street  ", "  Apt 4B  ", "  Nairobi  ", "  Nairobi  ", "  note  ",
		testAddressTime(),
	)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	if a.Recipient() != "Jane Wanjiku" || a.Line1() != "12 Kimathi Street" ||
		a.Town() != "Nairobi" || a.Line2() != "Apt 4B" ||
		a.County() != "Nairobi" || a.Notes() != "note" {
		t.Errorf("trim failed: %+v", a)
	}
}

func TestNewAddress_RejectsMissingRequired(t *testing.T) {
	cases := []struct {
		name      string
		recipient string
		phone     string
		line1     string
		town      string
		wantErr   error
	}{
		{"empty recipient", "", "+254712345678", "L1", "Town", ErrInvalidRecipient},
		{"empty phone", "R", "", "L1", "Town", ErrInvalidPhone},
		{"empty line1", "R", "+254712345678", "", "Town", ErrInvalidLine1},
		{"empty town", "R", "+254712345678", "L1", "", ErrInvalidTown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAddress(
				testAddressID(t), testUserID(t),
				tc.recipient, tc.phone, tc.line1, "", tc.town, "", "",
				testAddressTime(),
			)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestNewAddress_RejectsInvalidPhone(t *testing.T) {
	_, err := NewAddress(
		testAddressID(t), testUserID(t),
		"R", "not-a-phone", "L1", "", "Town", "", "",
		testAddressTime(),
	)
	if !errors.Is(err, ErrInvalidPhone) {
		t.Fatalf("want ErrInvalidPhone, got %v", err)
	}
}

func TestNewAddress_RejectsZeroIDs(t *testing.T) {
	_, err := NewAddress(
		ID{}, testUserID(t),
		"R", "+254712345678", "L1", "", "Town", "", "",
		testAddressTime(),
	)
	if !errors.Is(err, ErrZeroAddressID) {
		t.Fatalf("want ErrZeroAddressID, got %v", err)
	}

	_, err = NewAddress(
		testAddressID(t), ID{},
		"R", "+254712345678", "L1", "", "Town", "", "",
		testAddressTime(),
	)
	if !errors.Is(err, ErrZeroAddressUserID) {
		t.Fatalf("want ErrZeroAddressUserID, got %v", err)
	}
}

func TestNewAddress_RejectsZeroTime(t *testing.T) {
	_, err := NewAddress(
		testAddressID(t), testUserID(t),
		"R", "+254712345678", "L1", "", "Town", "", "",
		time.Time{},
	)
	if !errors.Is(err, ErrZeroAddressTime) {
		t.Fatalf("want ErrZeroAddressTime, got %v", err)
	}
}

func TestNewAddress_RejectsOversizedFields(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (Address, error)
	}{
		{"recipient too long", func() (Address, error) {
			return NewAddress(testAddressID(t), testUserID(t),
				strings.Repeat("r", maxRecipientLen+1), "+254712345678",
				"L1", "", "Town", "", "", testAddressTime())
		}},
		{"line1 too long", func() (Address, error) {
			return NewAddress(testAddressID(t), testUserID(t),
				"R", "+254712345678",
				strings.Repeat("l", maxLine1Len+1), "", "Town", "", "", testAddressTime())
		}},
		{"town too long", func() (Address, error) {
			return NewAddress(testAddressID(t), testUserID(t),
				"R", "+254712345678",
				"L1", "", strings.Repeat("t", maxTownLen+1), "", "", testAddressTime())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.fn(); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestNewAddress_TruncatesOversizedOptional(t *testing.T) {
	// Optional fields are truncated, not rejected — a long delivery note
	// shouldn't block the whole address.
	long := strings.Repeat("n", maxNotesLen+50)
	a, err := NewAddress(
		testAddressID(t), testUserID(t),
		"R", "+254712345678", "L1", "", "Town", "", long,
		testAddressTime(),
	)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	if len(a.Notes()) != maxNotesLen {
		t.Errorf("Notes length = %d, want %d", len(a.Notes()), maxNotesLen)
	}
}

func TestRehydrateAddress_RoundTrip(t *testing.T) {
	orig := mustNewAddress(t)
	got, err := RehydrateAddress(AddressRecord{
		ID:        orig.ID(),
		UserID:    orig.UserID(),
		Recipient: orig.Recipient(),
		PhoneE164: orig.PhoneE164(),
		Line1:     orig.Line1(),
		Line2:     orig.Line2(),
		Town:      orig.Town(),
		County:    orig.County(),
		Notes:     orig.Notes(),
		CreatedAt: orig.CreatedAt(),
	})
	if err != nil {
		t.Fatalf("RehydrateAddress: %v", err)
	}
	if orig != got {
		t.Fatalf("round trip differs:\n orig=%+v\n got=%+v", orig, got)
	}
}

func TestRehydrateAddress_RejectsInvalid(t *testing.T) {
	_, err := RehydrateAddress(AddressRecord{
		ID: testAddressID(t), UserID: testUserID(t),
		Recipient: "", PhoneE164: "+254712345678",
		Line1: "L1", Town: "Town", CreatedAt: testAddressTime(),
	})
	if !errors.Is(err, ErrInvalidRecipient) {
		t.Fatalf("want ErrInvalidRecipient, got %v", err)
	}
}
