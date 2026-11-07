// Package hashing provides argon2id password hashing with encoded-string
// format that embeds params, enabling rehash-on-login.
package hashing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	ErrPasswordTooLong     = errors.New("hashing: password exceeds maximum length")
	ErrInvalidHash         = errors.New("hashing: malformed hash")
	ErrIncompatibleVersion = errors.New("hashing: incompatible argon2 version")
	ErrInvalidParams       = errors.New("hashing: invalid params")
)

const (
	saltLen        = 16
	keyLen         = 32
	maxPasswordLen = 128

	// OWASP 2024 recommendation for argon2id: 19 MiB, 2 iterations, 1 thread.
	defaultMemory  uint32 = 19456
	defaultTime    uint32 = 2
	defaultThreads uint8  = 1
)

// Params are argon2id tuning parameters. Zero value is invalid; use
// DefaultParams.
type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
}

// DefaultParams returns the OWASP-recommended tuning.
func DefaultParams() Params {
	return Params{Memory: defaultMemory, Time: defaultTime, Threads: defaultThreads}
}

func (p Params) validate() error {
	if p.Memory < 8*1024 || p.Memory > 1<<20 {
		return fmt.Errorf("%w: memory=%d out of [8KiB, 1GiB]", ErrInvalidParams, p.Memory)
	}
	if p.Time < 1 || p.Time > 100 {
		return fmt.Errorf("%w: time=%d out of [1, 100]", ErrInvalidParams, p.Time)
	}
	if p.Threads < 1 || p.Threads > 64 {
		return fmt.Errorf("%w: threads=%d out of [1, 64]", ErrInvalidParams, p.Threads)
	}
	return nil
}

// Hash returns the encoded argon2id hash of password in the form
//
// $argon2id$v=19$m=19456,t=2,p=1$<base64-salt>$<base64-key>
//
// The encoded string is self-describing; Verify reads the params from it.
func Hash(password string, p Params) (string, error) {
	if len(password) > maxPasswordLen {
		return "", fmt.Errorf("%w: length %d", ErrPasswordTooLong, len(password))
	}
	if err := p.validate(); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hashing: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify checks password against encoded. It always uses the params embedded
// in encoded (never the current ones) so verification against old hashes is
// correct.
//
// Returns (match, needsRehash, err):
//   - match: password is correct
//   - needsRehash: match is true AND stored params differ from current —
//     caller should rehash password with current and persist.
//   - err: malformed hash or incompatible version. Not "wrong password";
//     that case returns (false, false, nil).
func Verify(password, encoded string, current Params) (bool, bool, error) {
	if len(password) > maxPasswordLen {
		return false, false, fmt.Errorf("%w: length %d", ErrPasswordTooLong, len(password))
	}
	if err := current.validate(); err != nil {
		return false, false, err
	}
	stored, salt, key, err := parseEncoded(encoded)
	if err != nil {
		return false, false, err
	}
	// #nosec G115 -- key is always keyLen (32) bytes.
	computed := argon2.IDKey([]byte(password), salt, stored.Time, stored.Memory, stored.Threads, uint32(len(key)))
	if subtle.ConstantTimeCompare(key, computed) != 1 {
		return false, false, nil
	}
	needsRehash := stored.Memory != current.Memory ||
		stored.Time != current.Time ||
		stored.Threads != current.Threads
	return true, needsRehash, nil
}

func parseEncoded(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrInvalidHash
	}
	var version, memory, iter int
	var threads int
	if n, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || n != 1 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("%w: v=%d want v=%d", ErrIncompatibleVersion, version, argon2.Version)
	}
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iter, &threads); err != nil || n != 3 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if memory <= 0 || iter <= 0 || threads <= 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	p := Params{Memory: uint32(memory), Time: uint32(iter), Threads: uint8(threads)}
	if err := p.validate(); err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: %s", ErrInvalidHash, err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if len(salt) != saltLen || len(key) != keyLen {
		return Params{}, nil, nil, ErrInvalidHash
	}
	return p, salt, key, nil
}
