package config

import (
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Env string

const (
	EnvLocal      Env = "local"
	EnvStaging    Env = "staging"
	EnvProduction Env = "production"
)

// LockoutStep is one entry in the escalating-backoff schedule.
type LockoutStep struct {
	MinFailures int
	Wait        time.Duration
}

// PreviousKey is a public key retained for verification during JWT rotation.
type PreviousKey struct {
	KID    string
	Public ed25519.PublicKey
}

type Config struct {
	Env                  Env           `env:"ENV"                    envDefault:"local"`
	HTTPAddr             string        `env:"HTTP_ADDR"              envDefault:":8080"`
	InternalAddr         string        `env:"INTERNAL_ADDR"          envDefault:":9090"`
	LogLevel             slog.Level    `env:"LOG_LEVEL"              envDefault:"info"`
	ShutdownTimeout      time.Duration `env:"SHUTDOWN_TIMEOUT"       envDefault:"30s"`
	ShutdownPropagation  time.Duration `env:"SHUTDOWN_PROPAGATION"   envDefault:"5s"`
	DatabaseURL          string        `env:"DATABASE_URL"`
	MigrationDatabaseURL string        `env:"MIGRATION_DATABASE_URL"`

	RedisCacheURL   string `env:"REDIS_CACHE_URL"   envDefault:"redis://localhost:6379/0"`
	RedisStreamsURL string `env:"REDIS_STREAMS_URL" envDefault:"redis://localhost:6380/0"`

	JWTPrivateKeyB64 string        `env:"JWT_PRIVATE_KEY_B64"`
	JWTSigningKID    string        `env:"JWT_SIGNING_KID"        envDefault:"kid-local"`
	JWTPPreviousKeys string        `env:"JWT_PREVIOUS_KEYS"`
	JWTIssuer        string        `env:"JWT_ISSUER"             envDefault:"storefront-backend"`
	JWTAudience      string        `env:"JWT_AUDIENCE"           envDefault:"storefront"`
	JWTAccessTTL     time.Duration `env:"JWT_ACCESS_TTL"         envDefault:"15m"`
	JWTRefreshTTL    time.Duration `env:"JWT_REFRESH_TTL"        envDefault:"168h"`

	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:","`
	TrustedProxyCIDRs  []string `env:"TRUSTED_PROXY_CIDRS"  envSeparator:","`

	RateLimitAuthCapacity   int           `env:"RATE_LIMIT_AUTH_CAPACITY"   envDefault:"5"`
	RateLimitAuthWindow     time.Duration `env:"RATE_LIMIT_AUTH_WINDOW"     envDefault:"1m"`
	RateLimitGlobalCapacity int           `env:"RATE_LIMIT_GLOBAL_CAPACITY" envDefault:"100"`
	RateLimitGlobalWindow   time.Duration `env:"RATE_LIMIT_GLOBAL_WINDOW"   envDefault:"1m"`

	LockoutSchedule  string        `env:"LOCKOUT_SCHEDULE"   envDefault:"2:100ms,4:500ms,6:2s,10:30s,20:5m"`
	LockoutRecordTTL time.Duration `env:"LOCKOUT_RECORD_TTL" envDefault:"24h"`
}

func Load() (*Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if c.JWTPrivateKeyB64 == "" && c.Env == EnvLocal {
		seed := make([]byte, ed25519.SeedSize)
		if _, err := crand.Read(seed); err != nil {
			return nil, fmt.Errorf("config: generate local JWT key: %w", err)
		}
		c.JWTPrivateKeyB64 = base64.StdEncoding.EncodeToString(seed)
		slog.Warn("config: generated ephemeral JWT signing key (local env; tokens invalidated on restart)")
	}
	if c.DatabaseURL == "" && c.Env == EnvLocal {
		c.DatabaseURL = "postgres://storefront:storefront@localhost:5432/storefront?sslmode=disable"
	}
	if len(c.CORSAllowedOrigins) == 0 && c.Env == EnvLocal {
		c.CORSAllowedOrigins = []string{"http://localhost:3000"}
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

func (c *Config) IsProduction() bool { return c.Env == EnvProduction }
func (c *Config) IsStaging() bool    { return c.Env == EnvStaging }
func (c *Config) IsLocal() bool      { return c.Env == EnvLocal }

// CookieSecure is true in staging and production only. Secure cookies
// require HTTPS, which isn't available in local dev.
func (c *Config) CookieSecure() bool {
	return c.Env == EnvStaging || c.Env == EnvProduction
}

func (c *Config) validate() error {
	switch c.Env {
	case EnvLocal, EnvStaging, EnvProduction:
	default:
		return fmt.Errorf("ENV must be one of local|staging|production, got %q", c.Env)
	}
	if c.HTTPAddr == "" || c.InternalAddr == "" {
		return fmt.Errorf("HTTP_ADDR and INTERNAL_ADDR must both be set")
	}
	if c.HTTPAddr == c.InternalAddr {
		return fmt.Errorf("HTTP_ADDR and INTERNAL_ADDR must differ")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be positive, got %s", c.ShutdownTimeout)
	}
	if c.ShutdownPropagation < 0 {
		return fmt.Errorf("SHUTDOWN_PROPAGATION must be non-negative, got %s", c.ShutdownPropagation)
	}
	if c.RedisCacheURL == "" || c.RedisStreamsURL == "" {
		return fmt.Errorf("REDIS_CACHE_URL and REDIS_STREAMS_URL required")
	}
	if _, err := c.ParseJWTSigningKey(); err != nil {
		return err
	}
	if c.JWTSigningKID == "" {
		return fmt.Errorf("JWT_SIGNING_KID required")
	}
	if _, err := c.ParseJWTPreviousKeys(); err != nil {
		return err
	}
	if c.JWTIssuer == "" || c.JWTAudience == "" {
		return fmt.Errorf("JWT_ISSUER and JWT_AUDIENCE required")
	}
	if c.JWTAccessTTL <= 0 || c.JWTRefreshTTL <= 0 {
		return fmt.Errorf("JWT TTLs must be positive")
	}
	if c.JWTAccessTTL >= c.JWTRefreshTTL {
		return fmt.Errorf("JWT_ACCESS_TTL must be < JWT_REFRESH_TTL")
	}
	if c.RateLimitAuthCapacity <= 0 || c.RateLimitGlobalCapacity <= 0 {
		return fmt.Errorf("RATE_LIMIT capacities must be positive")
	}
	if c.RateLimitAuthWindow <= 0 || c.RateLimitGlobalWindow <= 0 {
		return fmt.Errorf("RATE_LIMIT windows must be positive")
	}
	steps, err := c.ParseLockoutSchedule()
	if err != nil {
		return err
	}
	if maxWait := steps[len(steps)-1].Wait; c.LockoutRecordTTL < maxWait {
		return fmt.Errorf("LOCKOUT_RECORD_TTL %s < max wait %s", c.LockoutRecordTTL, maxWait)
	}
	if _, err := c.ParseCORSOrigins(); err != nil {
		return err
	}
	if _, err := c.ParseTrustedProxies(); err != nil {
		return err
	}
	if c.IsStaging() || c.IsProduction() {
		if c.DatabaseURL == "" {
			return fmt.Errorf("DATABASE_URL required in %s", c.Env)
		}
		if len(c.CORSAllowedOrigins) == 0 {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS required in %s", c.Env)
		}
		if len(c.TrustedProxyCIDRs) == 0 {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS required in %s", c.Env)
		}
	}
	return nil
}

func (c *Config) ParseJWTSigningKey() (ed25519.PrivateKey, error) {
	if c.JWTPrivateKeyB64 == "" {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY_B64 required (run 'make genkey' for local dev)")
	}
	seed, err := base64.StdEncoding.DecodeString(c.JWTPrivateKeyB64)
	if err != nil {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY_B64 invalid base64: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY_B64 must decode to %d bytes, got %d",
			ed25519.SeedSize, len(seed))
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func (c *Config) ParseJWTPreviousKeys() ([]PreviousKey, error) {
	if c.JWTPPreviousKeys == "" {
		return nil, nil
	}
	parts := strings.Split(c.JWTPPreviousKeys, ",")
	out := make([]PreviousKey, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("JWT_PREVIOUS_KEYS step %q invalid (want kid:base64pub)", p)
		}
		kid := strings.TrimSpace(kv[0])
		if kid == "" {
			return nil, fmt.Errorf("JWT_PREVIOUS_KEYS step %q: empty kid", p)
		}
		pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(kv[1]))
		if err != nil {
			return nil, fmt.Errorf("JWT_PREVIOUS_KEYS step %q: invalid base64", p)
		}
		if len(pub) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("JWT_PREVIOUS_KEYS step %q: pub must be %d bytes",
				p, ed25519.PublicKeySize)
		}
		out = append(out, PreviousKey{KID: kid, Public: ed25519.PublicKey(pub)})
	}
	return out, nil
}

func (c *Config) ParseTrustedProxies() ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(c.TrustedProxyCIDRs))
	for _, s := range c.TrustedProxyCIDRs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS %q: %w", s, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func (c *Config) ParseCORSOrigins() ([]string, error) {
	out := make([]string, 0, len(c.CORSAllowedOrigins))
	for _, s := range c.CORSAllowedOrigins {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if s == "*" {
			return nil, fmt.Errorf("CORS wildcard origin not allowed")
		}
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("CORS origin %q invalid", s)
		}
		out = append(out, s)
	}
	return out, nil
}

func (c *Config) ParseLockoutSchedule() ([]LockoutStep, error) {
	if c.LockoutSchedule == "" {
		return nil, fmt.Errorf("LOCKOUT_SCHEDULE required")
	}
	parts := strings.Split(c.LockoutSchedule, ",")
	out := make([]LockoutStep, 0, len(parts))
	prev := 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("LOCKOUT_SCHEDULE step %q invalid (want N:duration)", p)
		}
		n, err := strconv.Atoi(strings.TrimSpace(kv[0]))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("LOCKOUT_SCHEDULE step %q: invalid minFailures", p)
		}
		if n <= prev {
			return nil, fmt.Errorf("LOCKOUT_SCHEDULE step %q: minFailures must strictly increase", p)
		}
		prev = n
		wait, err := time.ParseDuration(strings.TrimSpace(kv[1]))
		if err != nil || wait <= 0 {
			return nil, fmt.Errorf("LOCKOUT_SCHEDULE step %q: invalid wait", p)
		}
		out = append(out, LockoutStep{MinFailures: n, Wait: wait})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("LOCKOUT_SCHEDULE produced no steps")
	}
	return out, nil
}
