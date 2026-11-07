package hashing

import (
"errors"
"fmt"
"math/rand/v2"
"strings"
"testing"
)

func TestHashVerify_RoundTrip(t *testing.T) {
cases := []string{
"",
"password",
"correct horse battery staple",
"p@ssw0rd!#$%",
"  spaces  ",
strings.Repeat("a", 128), // exactly at max
}
p := DefaultParams()
for _, pw := range cases {
t.Run(fmt.Sprintf("len=%d", len(pw)), func(t *testing.T) {
encoded, err := Hash(pw, p)
if err != nil {
t.Fatalf("Hash: %v", err)
}
ok, rehash, err := Verify(pw, encoded, p)
if err != nil {
t.Fatalf("Verify: %v", err)
}
if !ok {
t.Fatalf("verify failed for %q", pw)
}
if rehash {
t.Fatalf("freshly hashed should not need rehash")
}
})
}
}

func TestVerify_WrongPassword(t *testing.T) {
p := DefaultParams()
encoded, err := Hash("password", p)
if err != nil {
t.Fatal(err)
}
wrong := []string{
"Password",
"password ",
" password",
"passwore",
"",
strings.Repeat("x", 129),
}
for _, w := range wrong {
t.Run(w, func(t *testing.T) {
ok, rehash, err := Verify(w, encoded, p)
if err != nil {
if errors.Is(err, ErrPasswordTooLong) {
return // expected for over-length
}
t.Fatalf("Verify: %v", err)
}
if ok {
t.Fatalf("wrong password %q accepted", w)
}
if rehash {
t.Fatalf("wrong password set needsRehash")
}
})
}
}

func TestHash_UniqueSalts(t *testing.T) {
p := DefaultParams()
seen := make(map[string]bool)
for i := 0; i < 20; i++ {
encoded, err := Hash("password", p)
if err != nil {
t.Fatal(err)
}
if seen[encoded] {
t.Fatalf("iteration %d: same hash twice — salt not random", i)
}
seen[encoded] = true
}
}

func TestHash_RejectsOverMaxLength(t *testing.T) {
p := DefaultParams()
_, err := Hash(strings.Repeat("a", 129), p)
if !errors.Is(err, ErrPasswordTooLong) {
t.Fatalf("want ErrPasswordTooLong, got %v", err)
}
}

func TestHash_RejectsInvalidParams(t *testing.T) {
cases := map[string]Params{
"zero memory":  {Memory: 0, Time: 2, Threads: 1},
"tiny memory":  {Memory: 1024, Time: 2, Threads: 1},
"huge memory":  {Memory: 2 << 20, Time: 2, Threads: 1},
"zero time":    {Memory: 19456, Time: 0, Threads: 1},
"huge time":    {Memory: 19456, Time: 200, Threads: 1},
"zero threads": {Memory: 19456, Time: 2, Threads: 0},
"many threads": {Memory: 19456, Time: 2, Threads: 128},
}
for name, p := range cases {
t.Run(name, func(t *testing.T) {
if _, err := Hash("password", p); !errors.Is(err, ErrInvalidParams) {
t.Fatalf("want ErrInvalidParams, got %v", err)
}
})
}
}

func TestVerify_NeedsRehash(t *testing.T) {
oldParams := Params{Memory: 8 * 1024, Time: 1, Threads: 1}
newParams := DefaultParams()

encoded, err := Hash("password", oldParams)
if err != nil {
t.Fatal(err)
}

// Verifying with the NEW params must still match (uses stored params to
// recompute), but signal that rehash is due.
ok, rehash, err := Verify("password", encoded, newParams)
if err != nil {
t.Fatalf("Verify: %v", err)
}
if !ok {
t.Fatal("password should match")
}
if !rehash {
t.Fatal("needsRehash should be true when params differ")
}

// Rehash and confirm the new hash verifies without rehash signal.
newEncoded, err := Hash("password", newParams)
if err != nil {
t.Fatal(err)
}
ok, rehash, err = Verify("password", newEncoded, newParams)
if err != nil || !ok || rehash {
t.Fatalf("after rehash: ok=%t rehash=%t err=%v", ok, rehash, err)
}

// Wrong password against old hash: no match, no rehash signal.
ok, rehash, err = Verify("wrong", encoded, newParams)
if err != nil || ok || rehash {
t.Fatalf("wrong pw: ok=%t rehash=%t err=%v", ok, rehash, err)
}
}

func TestVerify_Malformed(t *testing.T) {
p := DefaultParams()
cases := map[string]string{
"empty":              "",
"not argon2":         "$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
"missing parts":      "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA",
"extra parts":        "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA$extra",
"bad version format": "$argon2id$v=abc$m=19456,t=2,p=1$c2FsdA$aGFzaA",
"bad params format":  "$argon2id$v=19$m=x,t=2,p=1$c2FsdA$aGFzaA",
"bad salt base64":    "$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",
"bad key base64":     "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$!!!",
"short salt":         "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE",
"wrong version":      "$argon2id$v=99$m=19456,t=2,p=1$c2FsdA$aGFzaA",
}
for name, enc := range cases {
t.Run(name, func(t *testing.T) {
_, _, err := Verify("password", enc, p)
if err == nil {
t.Fatalf("want error for malformed hash")
}
})
}
}

func TestVerify_IncompatibleVersion(t *testing.T) {
p := DefaultParams()
enc := "$argon2id$v=99$m=19456,t=2,p=1$c2FsdA$aGFzaA"
_, _, err := Verify("password", enc, p)
if !errors.Is(err, ErrIncompatibleVersion) {
t.Fatalf("want ErrIncompatibleVersion, got %v", err)
}
}

func TestHash_EmptyPasswordAllowed(t *testing.T) {
// Strength rules (min length, common password) are enforced by
// domain.ValidatePassword, not by the hashing layer.
p := DefaultParams()
encoded, err := Hash("", p)
if err != nil {
t.Fatalf("Hash(\"\"): %v", err)
}
ok, _, err := Verify("", encoded, p)
if err != nil || !ok {
t.Fatalf("empty password round trip failed: ok=%t err=%v", ok, err)
}
}

// Property: Hash then Verify always succeeds for any password in range.
func TestProperty_HashVerifyRoundTrip(t *testing.T) {
p := DefaultParams()
r := rand.New(rand.NewPCG(101, 102))
for i := 0; i < 50; i++ {
n := 1 + r.IntN(64)
pw := randomString(r, n)
encoded, err := Hash(pw, p)
if err != nil {
t.Fatalf("iter %d: Hash: %v", i, err)
}
ok, rehash, err := Verify(pw, encoded, p)
if err != nil || !ok || rehash {
t.Fatalf("iter %d: ok=%t rehash=%t err=%v", i, ok, rehash, err)
}
}
}

// Property: Verify rejects any single-character modification of the password.
func TestProperty_SingleCharChangeRejected(t *testing.T) {
p := DefaultParams()
encoded, err := Hash("password", p)
if err != nil {
t.Fatal(err)
}
for i := 0; i < len("password"); i++ {
mutated := []byte("password")
if mutated[i] == 'a' {
mutated[i] = 'b'
} else {
mutated[i] = 'a'
}
ok, _, err := Verify(string(mutated), encoded, p)
if err != nil {
t.Fatalf("pos %d: %v", i, err)
}
if ok {
t.Fatalf("pos %d: single-char change accepted", i)
}
}
}

func randomString(r *rand.Rand, n int) string {
const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
b := make([]byte, n)
for i := range b {
b[i] = alphabet[r.IntN(len(alphabet))]
}
return string(b)
}
