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
	"github.com/benkivuva/storefront-backend/internal/adapter/metrics"
	"github.com/benkivuva/storefront-backend/internal/adapter/payments"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	adapterredis "github.com/benkivuva/storefront-backend/internal/adapter/redis"
	"github.com/benkivuva/storefront-backend/internal/config"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/clock"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
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
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: worker <relay|consumer-email|cleanup|reaper|replay>")
	}
	role := os.Args[1]

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
	case "cleanup":
		err = runCleanup(ctx, pool)
	case "reaper":
		err = runReaper(ctx, pool, cfg)
	case "replay":
		err = runReplay(ctx, pool, rdb)
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
	metrics.NewServer(postgres.NewOutboxRepo(pool), rdb, ":9091").Start(ctx)
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
	metrics.NewServer(postgres.NewOutboxRepo(pool), rdb, ":9091").Start(ctx)
	err := rdb.XGroupCreateMkStream(ctx, "events:order", "email-group", "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("create consumer group: %w", err)
	}

	processedRepo := postgres.NewProcessedEventRepo(pool)
	userRepo := postgres.NewUserRepo(pool)
	reader := adapterredis.NewStreamReader(rdb, "events:order", "events:dlq")
	var mailerAdapter domain.Mailer
	smtpAddr := os.Getenv("SMTP_ADDR")
	if smtpAddr != "" {
		smtpFrom := os.Getenv("SMTP_FROM")
		mailerAdapter = mailer.NewSMTPMailer(smtpAddr, smtpFrom)
		slog.Info("using SMTP mailer", "addr", smtpAddr)
	} else {
		mailerAdapter = mailer.NewLogMailer()
		slog.Info("using log mailer (no SMTP_ADDR set)")
	}

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

func runCleanup(ctx context.Context, pool *pgxpool.Pool) error {
	outboxRepo := postgres.NewOutboxRepo(pool)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)

	slog.Info("starting outbox cleanup", "cutoff", cutoff)
	deleted, err := outboxRepo.DeletePublishedBefore(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("cleanup outbox: %w", err)
	}

	slog.Info("outbox cleanup complete", "rows_deleted", deleted)
	return nil
}

func runReplay(ctx context.Context, pool *pgxpool.Pool, rdb *goredis.Client) error {
	replayCmd := flag.NewFlagSet("replay", flag.ExitOnError)
	fromFlag := replayCmd.String("from", "", "Start time (RFC3339)")
	toFlag := replayCmd.String("to", "", "End time (RFC3339)")

	if err := replayCmd.Parse(os.Args[2:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *fromFlag == "" || *toFlag == "" {
		return fmt.Errorf("usage: worker replay --from=<RFC3339> --to=<RFC3339>")
	}

	from, err := time.Parse(time.RFC3339, *fromFlag)
	if err != nil {
		return fmt.Errorf("parse --from: %w", err)
	}
	to, err := time.Parse(time.RFC3339, *toFlag)
	if err != nil {
		return fmt.Errorf("parse --to: %w", err)
	}

	outboxRepo := postgres.NewOutboxRepo(pool)
	publisher := adapterredis.NewEventPublisher(rdb, "events")
	replaySvc := service.NewReplayService(outboxRepo, publisher)

	slog.Info("starting replay", "from", from, "to", to)
	count, err := replaySvc.Replay(ctx, from, to)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}

	slog.Info("replay complete", "events_replayed", count)
	return nil
}

func runReaper(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	gen := idgen.New()
	clk := clock.New()
	prov, err := payments.New(cfg.PaymentProvider)
	if err != nil {
		return fmt.Errorf("payment provider: %w", err)
	}

	orderRepo := postgres.NewOrderRepo(pool, gen)
	paymentRepo := postgres.NewPaymentRepo(pool)
	webhookRepo := postgres.NewWebhookRepo(pool, gen)

	svc := service.NewPaymentService(orderRepo, paymentRepo, prov, gen, clk)
	svc.SetWebhookRepo(webhookRepo)

	slog.Info("reaper started (running every 30 seconds)")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			reaped, err := svc.ReapExpiredOrders(ctx)
			if err != nil {
				slog.Error("reaper failed", "error", err)
			} else if reaped > 0 {
				slog.Info("reaped expired orders", "count", reaped)
			}
		}
	}
}
