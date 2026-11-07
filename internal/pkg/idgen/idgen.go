// Package idgen produces UUIDv7 identifiers and raw opaque tokens.
package idgen

import (
	"crypto/rand"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

// Generator satisfies both domain.IDGenerator and domain.RawTokenGenerator.
type Generator struct{}

func New() *Generator { return &Generator{} }

func (g *Generator) NewID() domain.ID {
	var id domain.ID
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("idgen: crypto/rand unavailable: " + err.Error())
	}
	ms := uint64(time.Now().UnixMilli())
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	id[6] = (b[0] & 0x0f) | 0x70
	id[7] = b[1]
	id[8] = (b[2] & 0x3f) | 0x80
	copy(id[9:], b[3:])
	return id
}

func (g *Generator) NewRawToken() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
