package config

import (
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/base64"
	"log/slog"
	"testing"
	"time"
)

func testJWTKey(t *testing.T) string {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	if _, err := crand.Read(seed); err != nil {
		t.Fatalf("generate seed: %v", err)
	}
	return base64.StdEncoding.EncodeToString(seed)
}

// stagingEnv sets the minimum env vars required for Load() to succeed in
// staging mode. Used by tests that need to override ENV=staging.
func stagingEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV", "staging")
	t.Setenv("JWT_PRIVATE_KEY_B64", testJWTKey(t))
	t.Setenv("DATABASE_URL", "postgres://x:y@localhost:5432/db")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://storefront.example.com")
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")
}

func TestLoad_Defaults(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.Env != EnvLocal {
		t.Errorf("Env = %q", c.Env)
	}
	if c.HTTPAddr != ":8080" || c.InternalAddr != ":9090" {
		t.Errorf("addrs = %q / %q", c.HTTPAddr, c.InternalAddr)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v", c.LogLevel)
	}
	if c.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s", c.ShutdownTimeout)
	}
	if c.JWTPrivateKeyB64 == "" {
		t.Error("local env must auto-generate JWT key")
	}
	if c.RedisCacheURL != "redis://localhost:6379/0" {
		t.Errorf("RedisCacheURL = %q", c.RedisCacheURL)
	}
	if c.RedisStreamsURL != "redis://localhost:6380/0" {
		t.Errorf("RedisStreamsURL = %q", c.RedisStreamsURL)
	}
	if c.JWTAccessTTL != 15*time.Minute {
		t.Errorf("JWTAccessTTL = %s", c.JWTAccessTTL)
	}
	if c.JWTRefreshTTL != 168*time.Hour {
		t.Errorf("JWTRefreshTTL = %s", c.JWTRefreshTTL)
	}
	if c.RateLimitAuthCapacity != 5 {
		t.Errorf("RateLimitAuthCapacity = %d", c.RateLimitAuthCapacity)
	}
}

func TestLoad_Overrides(t *testing.T) {
	stagingEnv(t)
	t.Setenv("HTTP_ADDR", ":18080")
	t.Setenv("INTERNAL_ADDR", ":19090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SHUTDOWN_TIMEOUT", "5s")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.Env != EnvStaging || c.HTTPAddr != ":18080" || c.InternalAddr != ":19090" {
		t.Errorf("fields = %+v", c)
	}
	if c.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", c.LogLevel)
	}
	if c.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %s", c.ShutdownTimeout)
	}
	if !c.CookieSecure() {
		t.Error("staging must use secure cookies")
	}
}

func TestLoad_LocalCookiesInsecure(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.CookieSecure() {
		t.Error("local must not use secure cookies")
	}
}

func TestLoad_Invalid(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"unknown ENV", map[string]string{"ENV": "prod"}},
		{"bogus log level", map[string]string{"LOG_LEVEL": "trace"}},
		{"zero shutdown", map[string]string{"SHUTDOWN_TIMEOUT": "0s"}},
		{"negative shutdown", map[string]string{"SHUTDOWN_TIMEOUT": "-1s"}},
		{"same addrs", map[string]string{"HTTP_ADDR": ":8080", "INTERNAL_ADDR": ":8080"}},
		{"zero auth capacity", map[string]string{"RATE_LIMIT_AUTH_CAPACITY": "0"}},
		{"bad lockout schedule", map[string]string{"LOCKOUT_SCHEDULE": "garbage"}},
		{"access ttl >= refresh ttl", map[string]string{
			"JWT_ACCESS_TTL": "8h", "JWT_REFRESH_TTL": "1h",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestLoad_StagingRequiresDatabaseURL(t *testing.T) {
	stagingEnv(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty DATABASE_URL in staging")
	}
}

func TestLoad_StagingRequiresCORS(t *testing.T) {
	stagingEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty CORS_ALLOWED_ORIGINS in staging")
	}
}

func TestLoad_StagingRequiresTrustedProxies(t *testing.T) {
	stagingEnv(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty TRUSTED_PROXY_CIDRS in staging")
	}
}

func TestLoad_StagingRequiresJWTKey(t *testing.T) {
	stagingEnv(t)
	t.Setenv("JWT_PRIVATE_KEY_B64", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty JWT_PRIVATE_KEY_B64 in staging")
	}
}

func TestParseJWTSigningKey_Valid(t *testing.T) {
	c := &Config{JWTPrivateKeyB64: testJWTKey(t)}
	key, err := c.ParseJWTSigningKey()
	if err != nil {
		t.Fatalf("ParseJWTSigningKey: %v", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		t.Errorf("key len = %d, want %d", len(key), ed25519.PrivateKeySize)
	}
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		t.Error("Public() returned wrong type/size")
	}
}

func TestParseJWTSigningKey_Errors(t *testing.T) {
	cases := map[string]string{
		"empty":      "",
		"bad base64": "not!!!base64",
		"too short":  base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"too long":   base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Config{JWTPrivateKeyB64: v}
			if _, err := c.ParseJWTSigningKey(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseJWTPreviousKeys_Empty(t *testing.T) {
	c := &Config{JWTPPreviousKeys: ""}
	keys, err := c.ParseJWTPreviousKeys()
	if err != nil || keys != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", keys, err)
	}
}

func TestParseJWTPreviousKeys_Valid(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(nil)
	pub2, _, _ := ed25519.GenerateKey(nil)
	s := "kid-old:" + base64.StdEncoding.EncodeToString(pub1) +
		"," + "kid-older:" + base64.StdEncoding.EncodeToString(pub2)
	c := &Config{JWTPPreviousKeys: s}
	keys, err := c.ParseJWTPreviousKeys()
	if err != nil {
		t.Fatalf("ParseJWTPreviousKeys: %v", err)
	}
	if len(keys) != 2 || keys[0].KID != "kid-old" || keys[1].KID != "kid-older" {
		t.Errorf("keys = %+v", keys)
	}
}

func TestParseJWTPreviousKeys_Errors(t *testing.T) {
	cases := map[string]string{
		"no colon":     "kidonly",
		"empty kid":    ":" + base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"bad base64":   "kid:not!!base64",
		"wrong length": "kid:" + base64.StdEncoding.EncodeToString(make([]byte, 16)),
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Config{JWTPPreviousKeys: v}
			if _, err := c.ParseJWTPreviousKeys(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseTrustedProxies(t *testing.T) {
	c := &Config{TrustedProxyCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16", ""}}
	ps, err := c.ParseTrustedProxies()
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	if len(ps) != 2 {
		t.Fatalf("len = %d, want 2 (empty entries skipped)", len(ps))
	}
}

func TestParseTrustedProxies_Invalid(t *testing.T) {
	c := &Config{TrustedProxyCIDRs: []string{"not-a-cidr"}}
	if _, err := c.ParseTrustedProxies(); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCORSOrigins(t *testing.T) {
	c := &Config{CORSAllowedOrigins: []string{
		"https://a.example.com",
		"http://localhost:3000",
		"",
	}}
	os, err := c.ParseCORSOrigins()
	if err != nil {
		t.Fatalf("ParseCORSOrigins: %v", err)
	}
	if len(os) != 2 {
		t.Fatalf("len = %d", len(os))
	}
}

func TestParseCORSOrigins_Errors(t *testing.T) {
	cases := map[string][]string{
		"wildcard":  {"*"},
		"no scheme": {"example.com"},
		"no host":   {"https://"},
	}
	for name, origins := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Config{CORSAllowedOrigins: origins}
			if _, err := c.ParseCORSOrigins(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseLockoutSchedule_Valid(t *testing.T) {
	c := &Config{LockoutSchedule: "2:100ms,4:500ms,6:2s"}
	steps, err := c.ParseLockoutSchedule()
	if err != nil {
		t.Fatalf("ParseLockoutSchedule: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("len = %d", len(steps))
	}
	if steps[0].MinFailures != 2 || steps[0].Wait != 100*time.Millisecond {
		t.Errorf("step[0] = %+v", steps[0])
	}
	if steps[2].MinFailures != 6 || steps[2].Wait != 2*time.Second {
		t.Errorf("step[2] = %+v", steps[2])
	}
}

func TestParseLockoutSchedule_Errors(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"no duration":      "2",
		"bad duration":     "2:xyz",
		"zero minFailures": "0:1s",
		"non-increasing":   "2:1s,2:2s",
		"decreasing":       "4:1s,2:2s",
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Config{LockoutSchedule: s}
			if _, err := c.ParseLockoutSchedule(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoad_LockoutRecordTTLTooSmall(t *testing.T) {
	t.Setenv("LOCKOUT_RECORD_TTL", "1s")
	t.Setenv("LOCKOUT_SCHEDULE", "2:5s")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for TTL < max wait")
	}
}

func TestCookieSecure(t *testing.T) {
	cases := []struct {
		env  Env
		want bool
	}{
		{EnvLocal, false},
		{EnvStaging, true},
		{EnvProduction, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.env), func(t *testing.T) {
			c := &Config{Env: tc.env}
			if got := c.CookieSecure(); got != tc.want {
				t.Errorf("CookieSecure() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsEnvironment(t *testing.T) {
	c := &Config{Env: EnvStaging}
	if !c.IsStaging() || c.IsProduction() || c.IsLocal() {
		t.Errorf("env helpers wrong for staging")
	}
}
