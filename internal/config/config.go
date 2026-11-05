package config

import (
"fmt"
"log/slog"
"time"

"github.com/caarlos0/env/v11"
)

// Env names a deployment environment.
type Env string

const (
EnvLocal      Env = "local"
EnvStaging    Env = "staging"
EnvProduction Env = "production"
)

// Config is the process-wide configuration, loaded once at startup.
// Add sections (DB, Redis, providers) when the adapters that read them exist.
type Config struct {
Env                 Env           `env:"ENV"                   envDefault:"local"`
HTTPAddr            string        `env:"HTTP_ADDR"             envDefault:":8080"`
InternalAddr        string        `env:"INTERNAL_ADDR"         envDefault:":9090"`
LogLevel            slog.Level    `env:"LOG_LEVEL"             envDefault:"info"`
ShutdownTimeout     time.Duration `env:"SHUTDOWN_TIMEOUT"      envDefault:"30s"`
ShutdownPropagation time.Duration `env:"SHUTDOWN_PROPAGATION"  envDefault:"5s"`
}

// Load reads configuration from the environment and validates it. The caller
// (main) must exit on error before serving traffic.
func Load() (*Config, error) {
var c Config
if err := env.Parse(&c); err != nil {
return nil, fmt.Errorf("config: %w", err)
}
if err := c.validate(); err != nil {
return nil, fmt.Errorf("config: %w", err)
}
return &c, nil
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
return nil
}
