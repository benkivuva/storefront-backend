package idgen

import (
	"sync"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

var _ domain.IDGenerator = (*Generator)(nil)
var _ domain.RawTokenGenerator = (*Generator)(nil)

func TestNewID_NotZero(t *testing.T) {
	id := New().NewID()
	if id.IsZero() {
		t.Fatal("generated id is zero")
	}
}

func TestNewID_VersionAndVariant(t *testing.T) {
	id := New().NewID()
	if v := id[6] >> 4; v != 0x7 {
		t.Errorf("version nibble = %d, want 7", v)
	}
	if v := id[8] >> 6; v != 0b10 {
		t.Errorf("variant bits = %b, want 10", v)
	}
}

func TestNewID_Unique(t *testing.T) {
	g := New()
	const N = 1000
	seen := make(map[domain.ID]struct{}, N)
	for i := 0; i < N; i++ {
		id := g.NewID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id at iteration %d", i)
		}
		seen[id] = struct{}{}
	}
}

func TestNewID_PrefixIsTimestamp(t *testing.T) {
	// Two IDs generated in the same millisecond share the first 6 bytes.
	// We can't assert strict ordering across ms boundaries without flaking,
	// but the low-order random bytes must differ.
	g := New()
	a := g.NewID()
	b := g.NewID()
	if a == b {
		t.Fatal("two consecutive IDs are identical")
	}
	if string(a[9:]) == string(b[9:]) {
		t.Fatal("random tail bytes did not differ")
	}
}

func TestNewRawToken_Length(t *testing.T) {
	raw, err := New().NewRawToken()
	if err != nil {
		t.Fatalf("NewRawToken: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("len = %d, want 32", len(raw))
	}
}

func TestNewRawToken_Unique(t *testing.T) {
	g := New()
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		raw, err := g.NewRawToken()
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if _, dup := seen[string(raw)]; dup {
			t.Fatalf("iter %d: duplicate token", i)
		}
		seen[string(raw)] = struct{}{}
	}
}

func TestNewID_Concurrent(t *testing.T) {
	g := New()
	var wg sync.WaitGroup
	const N = 200
	ids := make([]domain.ID, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i] = g.NewID()
		}(i)
	}
	wg.Wait()
	seen := make(map[domain.ID]struct{}, N)
	for i, id := range ids {
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id at index %d", i)
		}
		seen[id] = struct{}{}
	}
}
