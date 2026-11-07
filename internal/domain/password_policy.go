package domain

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrPasswordTooShort = errors.New("password policy: password too short")
	ErrPasswordTooLong  = errors.New("password policy: password too long")
	ErrPasswordCommon   = errors.New("password policy: password is too common")
)

const (
	PasswordMinLength = 8
	PasswordMaxLength = 128
)

// ValidatePassword returns nil if password is acceptable. Composition rules
// (uppercase/digit/symbol) are deliberately not enforced — NIST SP 800-63B
// is that they make passwords worse without adding security.
func ValidatePassword(password string) error {
	if len(password) < PasswordMinLength {
		return fmt.Errorf("%w: min %d, got %d", ErrPasswordTooShort, PasswordMinLength, len(password))
	}
	if len(password) > PasswordMaxLength {
		return fmt.Errorf("%w: max %d, got %d", ErrPasswordTooLong, PasswordMaxLength, len(password))
	}
	if _, ok := commonPasswords[strings.ToLower(password)]; ok {
		return ErrPasswordCommon
	}
	return nil
}

var commonPasswords = map[string]struct{}{
	"password":      {},
	"password1":     {},
	"password12":    {},
	"password123":   {},
	"password1234":  {},
	"passw0rd":      {},
	"p@ssw0rd":      {},
	"p@ssword":      {},
	"pass":          {},
	"passwd":        {},
	"12345678":      {},
	"123456789":     {},
	"1234567890":    {},
	"123456":        {},
	"12345":         {},
	"1234":          {},
	"1234567":       {},
	"11111111":      {},
	"00000000":      {},
	"87654321":      {},
	"654321":        {},
	"121212":        {},
	"123123":        {},
	"abc12345":      {},
	"abcd1234":      {},
	"qwerty":        {},
	"qwerty1":       {},
	"qwerty12":      {},
	"qwerty123":     {},
	"qwertyui":      {},
	"qwertyuiop":    {},
	"asdfgh":        {},
	"asdfghjkl":     {},
	"zxcvbnm":       {},
	"1qaz2wsx":      {},
	"1q2w3e4r":      {},
	"letmein":       {},
	"letmein1":      {},
	"welcome":       {},
	"welcome1":      {},
	"welcome123":    {},
	"admin":         {},
	"admin1":        {},
	"admin123":      {},
	"administrator": {},
	"root":          {},
	"toor":          {},
	"default":       {},
	"iloveyou":      {},
	"monkey":        {},
	"monkey1":       {},
	"dragon":        {},
	"dragon1":       {},
	"sunshine":      {},
	"princess":      {},
	"football":      {},
	"baseball":      {},
	"shadow":        {},
	"master":        {},
	"michael":       {},
	"jennifer":      {},
	"superman":      {},
	"batman":        {},
	"trustno1":      {},
	"starwars":      {},
	"whatever":      {},
	"freedom":       {},
	"changeme":      {},
	"temp1234":      {},
	"test1234":      {},
	"testpass":      {},
	"guest":         {},
	"user":          {},
	"login":         {},
	"secret":        {},
	"secret123":     {},
}
