package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/benkivuva/storefront-backend/internal/adapter/mailer"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	adapterredis "github.com/benkivuva/storefront-backend/internal/adapter/redis"
	"github.com/benkivuva/storefront-backend/internal/config"
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
	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		return fmt.Errorf("usage: worker <relay|consumer-email>")
	}
	role := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := slog.New(logging.NewJSON(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}

	streamsOpts, err := goredis.ParseURL(cfg.RedisStreamsURL)
	if err != nil {
		return fmt.Errorf("redis streams url: %w", err)
	}
	rdb := goredis.NewClient(streamsOpts)
	defer func() { _ = rdb.Close() }()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis streams ping: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	switch role {
	case "relay":
		err = runRelay(ctx, pool, rdb, sigCh)
	case "consumer-email":
		err = runEmailConsumer(ctx, pool, rdb, sigCh)
	default:
		return fmt.Errorf("unknown role: %s", role)
	}

	if err != nil {
		return err
	}

	slog.Info("shutting down", "role", role)
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}

func runRelay(ctx context.Context, pool *pgxpool.Pool, rdb *goredis.Client, sigCh chan os.Signal) error {
	outboxRepo := postgres.NewOutboxRepo(pool)
	publisher := adapterredis.NewEventPublisher(rdb, "events")
	relay := service.NewOutboxRelay(outboxRepo, publisher)

	slog.Info("relay started")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sigCh:
			return nil
		case <-ticker.C:
			if err := relay.ProcessBatch(ctx, 100); err != nil {
				slog.Error("relay process batch", "error", err)
			}
		}
	}
}

func runEmailConsumer(ctx context.Context, pool *pgxpool.Pool, rdb *goredis.Client, sigCh chan os.Signal) error {
	err := rdb.XGroupCreateMkStream(ctx, "events:order", "email-group", "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("create consumer group: %w", err)
	}

	processedRepo := postgres.NewProcessedEventRepo(pool)
	userRepo := postgres.NewUserRepo(pool)
	reader := adapterredis.NewStreamReader(rdb, "events:order", "events:dlq")
	mailerAdapter := mailer.NewLogMailer()

	emailHandler := service.NewEmailHandler(mailerAdapter, userRepo)
	consumer := service.NewConsumer("email-group", "worker-1", reader, processedRepo, emailHandler)

	slog.Info("email consumer started")
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sigCh:
			return nil
		default:
			if err := consumer.ProcessNextBatch(ctx, 10); err != nil {
				slog.Error("consumer process batch", "error", err)
				time.Sleep(1 * time.Second)
			}
		}
	}
}
