package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/benkivuva/storefront-backend/internal/adapter/hashingadapter"
	httpsrv "github.com/benkivuva/storefront-backend/internal/adapter/http"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/handlers"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	adapterredis "github.com/benkivuva/storefront-backend/internal/adapter/redis"
	"github.com/benkivuva/storefront-backend/internal/config"
	"github.com/benkivuva/storefront-backend/internal/pkg/clock"
	"github.com/benkivuva/storefront-backend/internal/pkg/hashing"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
	pkggjwt "github.com/benkivuva/storefront-backend/internal/pkg/jwt"
	"github.com/benkivuva/storefront-backend/internal/pkg/logging"
	"github.com/benkivuva/storefront-backend/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := slog.New(logging.NewJSON(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	logger.Info("database connected")

	cacheClient, err := parseRedis(cfg.RedisCacheURL)
	if err != nil {
		return fmt.Errorf("redis cache: %w", err)
	}
	defer func() { _ = cacheClient.Close() }()
	if err := cacheClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis cache ping: %w", err)
	}

	streamsClient, err := parseRedis(cfg.RedisStreamsURL)
	if err != nil {
		return fmt.Errorf("redis streams: %w", err)
	}
	defer func() { _ = streamsClient.Close() }()
	if err := streamsClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis streams ping: %w", err)
	}
	logger.Info("redis connected")

	signingKey, err := cfg.ParseJWTSigningKey()
	if err != nil {
		return fmt.Errorf("jwt signing key: %w", err)
	}
	cfgPrev, err := cfg.ParseJWTPreviousKeys()
	if err != nil {
		return fmt.Errorf("jwt previous keys: %w", err)
	}
	jwtPrev := make([]pkggjwt.PreviousKey, len(cfgPrev))
	for i, p := range cfgPrev {
		jwtPrev[i] = pkggjwt.PreviousKey{KID: p.KID, Public: p.Public}
	}
	ks, err := pkggjwt.NewKeySet(cfg.JWTSigningKID, signingKey, cfg.JWTIssuer, cfg.JWTAudience, jwtPrev)
	if err != nil {
		return fmt.Errorf("jwt keyset: %w", err)
	}

	trustedProxies, err := cfg.ParseTrustedProxies()
	if err != nil {
		return fmt.Errorf("trusted proxies: %w", err)
	}
	origins, err := cfg.ParseCORSOrigins()
	if err != nil {
		return fmt.Errorf("cors origins: %w", err)
	}

	cfgSteps, err := cfg.ParseLockoutSchedule()
	if err != nil {
		return fmt.Errorf("lockout schedule: %w", err)
	}
	adapterSteps := make([]adapterredis.BackoffStep, len(cfgSteps))
	for i, s := range cfgSteps {
		adapterSteps[i] = adapterredis.BackoffStep{MinFailures: s.MinFailures, Wait: s.Wait}
	}
	lockout, err := adapterredis.NewLockoutStore(cacheClient, adapterSteps, cfg.LockoutRecordTTL)
	if err != nil {
		return fmt.Errorf("lockout store: %w", err)
	}

	authRedisLimiter, err := adapterredis.NewLimiter(cacheClient, cfg.RateLimitAuthCapacity, cfg.RateLimitAuthWindow)
	if err != nil {
		return fmt.Errorf("auth limiter: %w", err)
	}
	authMemLimiter, err := adapterredis.NewMemoryLimiter(cfg.RateLimitAuthCapacity, cfg.RateLimitAuthWindow)
	if err != nil {
		return fmt.Errorf("auth memory limiter: %w", err)
	}
	globalRedisLimiter, err := adapterredis.NewLimiter(cacheClient, cfg.RateLimitGlobalCapacity, cfg.RateLimitGlobalWindow)
	if err != nil {
		return fmt.Errorf("global limiter: %w", err)
	}
	globalMemLimiter, err := adapterredis.NewMemoryLimiter(cfg.RateLimitGlobalCapacity, cfg.RateLimitGlobalWindow)
	if err != nil {
		return fmt.Errorf("global memory limiter: %w", err)
	}

	userRepo := postgres.NewUserRepo(pool)
	refreshRepo := postgres.NewRefreshTokenRepo(pool)
	addressRepo := postgres.NewAddressRepo(pool)
	productRepo := postgres.NewProductRepo(pool)
	categoryRepo := postgres.NewCategoryRepo(pool)
	inventoryRepo := postgres.NewInventoryRepo(pool)
	outboxRepo := postgres.NewOutboxRepo(pool)
	auditLog := postgres.NewAuditLog(pool)

	hasher := hashingadapter.NewHasher(hashing.DefaultParams())
	signer := middleware.NewJWTSigner(ks)
	clk := clock.New()
	gen := idgen.New()

	// Cache (using the redis-cache client, not the streams client)
	cache := adapterredis.NewCache(cacheClient, "storefront")

	// Catalog Service
	catalogSvc := service.NewCatalogService(
		productRepo, categoryRepo, inventoryRepo, outboxRepo, auditLog, cache, gen, clk,
	)
	svc, err := service.NewAuthService(
		userRepo, refreshRepo, addressRepo, auditLog, lockout,
		hasher, signer, clk, gen, gen,
		service.Config{AccessTTL: cfg.JWTAccessTTL, RefreshTTL: cfg.JWTRefreshTTL},
	)
	if err != nil {
		return fmt.Errorf("auth service: %w", err)
	}

	authHandler := handlers.NewAuthHandler(svc, trustedProxies, cfg.JWTRefreshTTL, cfg.CookieSecure())
	meHandler := handlers.NewMeHandler(svc)
	catalogHandler := handlers.NewCatalogHandler(catalogSvc)

	ready := httpsrv.NewReady()
	publicRouter := httpsrv.NewPublicRouter(httpsrv.RouterConfig{
		AuthHandler:    authHandler,
		MeHandler:      meHandler,
		CatalogHandler: catalogHandler,
		JWTVerifier:    middleware.NewJWTVerifier(ks),
		AuthLimiter:    rateLimiterAdapter{inner: authRedisLimiter},
		AuthFallback:   rateLimiterAdapter{inner: authMemLimiter},
		GlobalLimiter:  rateLimiterAdapter{inner: globalRedisLimiter},
		GlobalFallback: rateLimiterAdapter{inner: globalMemLimiter},
		TrustedProxies: trustedProxies,
		AllowedOrigins: origins,
		AuthCapacity:   cfg.RateLimitAuthCapacity,
		GlobalCapacity: cfg.RateLimitGlobalCapacity,
		Prod:           cfg.IsProduction(),
	})
	internalRouter := httpsrv.NewInternalRouter(ready)

	publicSrv, err := httpsrv.New(cfg.HTTPAddr, publicRouter, logger)
	if err != nil {
		return fmt.Errorf("public: %w", err)
	}
	internalSrv, err := httpsrv.New(cfg.InternalAddr, internalRouter, logger)
	if err != nil {
		return fmt.Errorf("internal: %w", err)
	}

	errCh := make(chan error, 2)
	go func() { errCh <- publicSrv.Serve() }()
	go func() { errCh <- internalSrv.Serve() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("signal received", "signal", sig.String())
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	}

	ready.MarkNotReady()
	logger.Info("marked not-ready", "propagation", cfg.ShutdownPropagation)
	if cfg.ShutdownPropagation > 0 {
		time.Sleep(cfg.ShutdownPropagation)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	var shutdownErr error
	for _, srv := range []*httpsrv.Server{publicSrv, internalSrv} {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	if shutdownErr != nil {
		return fmt.Errorf("shutdown: %w", shutdownErr)
	}
	logger.Info("shutdown complete")
	return nil
}

func parseRedis(rawURL string) (*goredis.Client, error) {
	opts, err := goredis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return goredis.NewClient(opts), nil
}

type rateLimiterAdapter struct {
	inner interface {
		Allow(ctx context.Context, key string, cost int) (adapterredis.Decision, error)
	}
}

func (r rateLimiterAdapter) Allow(ctx context.Context, key string, cost int) (middleware.Decision, error) {
	d, err := r.inner.Allow(ctx, key, cost)
	if err != nil {
		return middleware.Decision{}, err
	}
	return middleware.Decision{
		Allowed:    d.Allowed,
		Remaining:  d.Remaining,
		RetryAfter: d.RetryAfter,
	}, nil
}
