package hashingadapter

import (
	"testing"

	"github.com/benkivuva/storefront-backend/internal/domain"
	pkghashing "github.com/benkivuva/storefront-backend/internal/pkg/hashing"
)

var _ domain.PasswordHasher = (*Hasher)(nil)

func fastParams() pkghashing.Params {
	return pkghashing.Params{Memory: 8 * 1024, Time: 1, Threads: 1}
}

func TestHasher_RoundTrip(t *testing.T) {
	h := NewHasher(fastParams())
	encoded, err := h.Hash("correct-horse")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if encoded == "" {
		t.Fatal("empty hash")
	}
	match, needsRehash, err := h.Verify("correct-horse", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !match {
		t.Fatal("correct password rejected")
	}
	if needsRehash {
		t.Fatal("fresh hash should not need rehash")
	}
}

func TestHasher_WrongPassword(t *testing.T) {
	h := NewHasher(fastParams())
	encoded, _ := h.Hash("correct-horse")
	match, _, err := h.Verify("wrong", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if match {
		t.Fatal("wrong password accepted")
	}
}

func TestHasher_RehashSignal(t *testing.T) {
	// Hash with old params, verify with new; expect needsRehash true.
	oldHasher := NewHasher(pkghashing.Params{Memory: 8 * 1024, Time: 1, Threads: 1})
	newHasher := NewHasher(pkghashing.Params{Memory: 16 * 1024, Time: 2, Threads: 1})

	encoded, err := oldHasher.Hash("pw")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	match, needsRehash, err := newHasher.Verify("pw", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !match {
		t.Fatal("password should match")
	}
	if !needsRehash {
		t.Fatal("params differ; needsRehash should be true")
	}
}
