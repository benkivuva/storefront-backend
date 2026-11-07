package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const (
	sampleID = "01234567-89ab-7def-8123-456789abcdef"
	otherID  = "fedcba98-7654-7210-8edc-ba9876543210"
)

func mustID(t *testing.T, s string) ID {
	t.Helper()
	id, err := ParseID(s)
	if err != nil {
		t.Fatalf("ParseID(%q): %v", s, err)
	}
	return id
}

func TestID_IsZero(t *testing.T) {
	if !(ID{}).IsZero() {
		t.Fatal("zero ID must report IsZero")
	}
	if mustID(t, sampleID).IsZero() {
		t.Fatal("non-zero ID must not report IsZero")
	}
}

func TestID_StringRoundTrip(t *testing.T) {
	for _, s := range []string{
		"00000000-0000-0000-0000-000000000000",
		sampleID,
		otherID,
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
	} {
		if got := mustID(t, s).String(); got != s {
			t.Errorf("round trip: %q -> %q", s, got)
		}
	}
}

func TestParseID_NormalizesCase(t *testing.T) {
	if got := mustID(t, strings.ToUpper(sampleID)).String(); got != sampleID {
		t.Fatalf("got %q, want %q", got, sampleID)
	}
}

// Parsing and formatting could be wrong in the same way and still round-trip,
// so check a few raw bytes too.
func TestID_ByteLayout(t *testing.T) {
	id := mustID(t, sampleID)
	if id[0] != 0x01 || id[6] != 0x7d || id[15] != 0xef {
		t.Fatalf("unexpected bytes: %x", id[:])
	}
}

func TestParseID_RejectsMalformed(t *testing.T) {
	for name, s := range map[string]string{
		"empty":          "",
		"too short":      sampleID[:35],
		"too long":       sampleID + "f",
		"no dashes":      strings.ReplaceAll(sampleID, "-", ""),
		"misplaced dash": "012345678-9ab-7def-8123-456789abcdef",
		"non-hex":        "ZZZZZZZZ-89ab-7def-8123-456789abcdef",
		"padded":         " " + sampleID,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseID(s); !errors.Is(err, ErrInvalidID) {
				t.Fatalf("want ErrInvalidID, got %v", err)
			}
		})
	}
}

func TestID_JSON(t *testing.T) {
	type payload struct {
		ID ID `json:"id"`
	}
	in := payload{ID: mustID(t, sampleID)}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":"` + sampleID + `"}`; string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
	var out payload
	if err := json.Unmarshal(b, &out); err != nil || out != in {
		t.Fatalf("round trip: %+v, %v", out, err)
	}
	if err := json.Unmarshal([]byte(`{"id":"nope"}`), &out); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("want ErrInvalidID, got %v", err)
	}
}
