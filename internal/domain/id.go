package domain

import (
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrInvalidID = errors.New("domain: invalid id")

// ID is a 16-byte, UUID-shaped identifier. The domain owns the type so it
// needs no UUID library.
type ID [16]byte

// IDGenerator mints new IDs. The implementation lives in internal/pkg/idgen.
type IDGenerator interface {
	NewID() ID
}

func (id ID) IsZero() bool { return id == ID{} }

// String returns the lowercase 8-4-4-4-12 form.
func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

// ParseID parses the 8-4-4-4-12 form, in upper or lower case.
func ParseID(s string) (ID, error) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return ID{}, fmt.Errorf("%w: %q", ErrInvalidID, s)
	}
	b, err := hex.DecodeString(s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36])
	if err != nil {
		return ID{}, fmt.Errorf("%w: %q", ErrInvalidID, s)
	}
	var id ID
	copy(id[:], b)
	return id, nil
}

func (id ID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

func (id *ID) UnmarshalText(b []byte) error {
	parsed, err := ParseID(string(b))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
