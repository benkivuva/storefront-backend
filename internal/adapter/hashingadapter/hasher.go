// Package hashingadapter wraps internal/pkg/hashing as a domain.PasswordHasher.
package hashingadapter

import (
	pkghashing "github.com/benkivuva/storefront-backend/internal/pkg/hashing"
)

type Hasher struct {
	params pkghashing.Params
}

func NewHasher(p pkghashing.Params) *Hasher {
	return &Hasher{params: p}
}

func (h *Hasher) Hash(password string) (string, error) {
	return pkghashing.Hash(password, h.params)
}

func (h *Hasher) Verify(password, encoded string) (match, needsRehash bool, err error) {
	return pkghashing.Verify(password, encoded, h.params)
}
