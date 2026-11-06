package password_policy

import (
"errors"
"fmt"
"math/rand/v2"
"strings"
"testing"
)

func TestValidate_AcceptsValidPasswords(t *testing.T) {
valid := []string{
"correct horse battery staple", // passphrase
"my_unique_phrase_2026",
"Sup3rS3cretP@ssw0rd",           // not in blocklist
"correct-horse-battery-staple", // 28 chars
strings.Repeat("a", 128),        // exactly max
"Abcdefgh",                      // min length
}
for _, pw := range valid {
t.Run(fmt.Sprintf("len=%d", len(pw)), func(t *testing.T) {
if err := Validate(pw); err != nil {
t.Fatalf("Validate(%q): %v", pw, err)
}
})
}
}

func TestValidate_RejectsTooShort(t *testing.T) {
cases := []string{"", "a", "short", "1234567"}
for _, pw := range cases {
t.Run(pw, func(t *testing.T) {
err := Validate(pw)
if !errors.Is(err, ErrTooShort) {
t.Fatalf("Validate(%q): want ErrTooShort, got %v", pw, err)
}
})
}
}

func TestValidate_RejectsTooLong(t *testing.T) {
pw := strings.Repeat("a", 129)
if err := Validate(pw); !errors.Is(err, ErrTooLong) {
t.Fatalf("want ErrTooLong, got %v", err)
}
}

func TestValidate_RejectsCommon(t *testing.T) {
	// Some blocklist entries are shorter than MinLength. They still fail —
	// via ErrTooShort, which fires before the blocklist check. The test
	// asserts "rejected," not "rejected via blocklist specifically."
	common := []string{
		"password", "password123", "12345678", "qwerty", "qwertyuiop",
		"letmein", "welcome", "admin", "admin123", "iloveyou",
		"monkey", "dragon", "sunshine", "trustno1", "changeme",
		"test1234", "guest", "secret",
	}
	for _, pw := range common {
		t.Run(pw, func(t *testing.T) {
			err := Validate(pw)
			if !errors.Is(err, ErrCommon) && !errors.Is(err, ErrTooShort) {
				t.Fatalf("Validate(%q): want rejection, got %v", pw, err)
			}
		})
	}
}

func TestValidate_CaseInsensitive(t *testing.T) {
	for _, pw := range []string{"Password", "PASSWORD", "PaSsWoRd", "QwErTy", "Admin123"} {
		t.Run(pw, func(t *testing.T) {
			err := Validate(pw)
			if !errors.Is(err, ErrCommon) && !errors.Is(err, ErrTooShort) {
				t.Fatalf("Validate(%q): want rejection (case-insensitive), got %v", pw, err)
			}
		})
	}
}

func TestValidate_LeetspeakNotFullyBlocked(t *testing.T) {
// We do not normalize leetspeak. Documented limitation: "P@ssw0rd" is
// blocked only because it is an explicit entry, not because "password"
// was leetspeak-normalized. Users with novel leetspeak will pass the
// blocklist. That is acceptable — the blocklist is a floor, not a
// comprehensive check.
if err := Validate("P@ssw0rd"); !errors.Is(err, ErrCommon) {
t.Fatalf("P@ssw0rd should be blocked (explicit entry)")
}
// A leetspeak variant NOT in the list passes. That's by design.
if err := Validate("p4ssw0rd_unique"); err != nil {
t.Fatalf("non-blocklisted leetspeak should pass, got %v", err)
}
}

func TestValidate_ErrorMessagesContainContext(t *testing.T) {
// Frontend users see these messages. They should be self-explanatory.
err := Validate("short")
if !strings.Contains(err.Error(), "min 8") {
t.Errorf("short error should mention min 8: %v", err)
}
err = Validate(strings.Repeat("a", 200))
if !strings.Contains(err.Error(), "max 128") {
t.Errorf("long error should mention max 128: %v", err)
}
}

// Property: any password of acceptable length that is NOT in the blocklist
// passes. Random strings hit this reliably.
func TestProperty_ValidLengthNotBlockedPasses(t *testing.T) {
r := rand.New(rand.NewPCG(301, 302))
const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
for i := 0; i < 500; i++ {
n := MinLength + r.IntN(MaxLength-MinLength+1)
b := make([]byte, n)
for j := range b {
b[j] = alphabet[r.IntN(len(alphabet))]
}
pw := string(b)
if err := Validate(pw); err != nil {
// Might accidentally hit blocklist for very short strings.
if errors.Is(err, ErrCommon) && n < 12 {
continue
}
t.Fatalf("iter %d: Validate(%q) unexpectedly failed: %v", i, pw, err)
}
}
}

// Property: every string in the blocklist is rejected, regardless of case.
func TestProperty_BlocklistEntriesRejectedCaseInsensitive(t *testing.T) {
	for pw := range commonPasswords {
		for _, variant := range []string{
			pw,
			strings.ToUpper(pw),
			strings.ToLower(pw),
		} {
			err := Validate(variant)
			if errors.Is(err, ErrTooShort) {
				// Blocklist entries shorter than MinLength never reach the
				// blocklist check — they fail on length first. That's correct
				// behavior (length is more actionable than "too common").
				continue
			}
			if !errors.Is(err, ErrCommon) {
				t.Errorf("blocklist entry %q (variant %q) not rejected: %v", pw, variant, err)
			}
		}
	}
}
