package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	goredis "github.com/redis/go-redis/v9"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type Server struct {
	outbox domain.OutboxRepository
	rdb    *goredis.Client
	addr   string

	outboxOldestPending prometheus.Gauge
	dlqDepth            prometheus.Gauge
}

func NewServer(outbox domain.OutboxRepository, rdb *goredis.Client, addr string) *Server {
	s := &Server{
		outbox: outbox,
		rdb:    rdb,
		addr:   addr,
		outboxOldestPending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_oldest_pending_seconds",
			Help: "Age of the oldest pending outbox event in seconds",
		}),
		dlqDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dlq_depth",
			Help: "Number of messages in the Dead Letter Queue",
		}),
	}
	prometheus.MustRegister(s.outboxOldestPending, s.dlqDepth)
	return s
}

func (s *Server) Start(ctx context.Context) {
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())

		srv := &http.Server{Addr: s.addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
		go func() {
			slog.Info("metrics server started", "addr", s.addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server", "error", err)
			}
		}()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				_ = srv.Close()
				return
			case <-ticker.C:
				s.collect(ctx)
			}
		}
	}()
}

func (s *Server) collect(ctx context.Context) {
	oldest, err := s.outbox.OldestPending(ctx)
	if err == nil && !oldest.IsZero() {
		s.outboxOldestPending.Set(time.Since(oldest).Seconds())
	} else if err == nil {
		s.outboxOldestPending.Set(0)
	}

	dlqLen, err := s.rdb.XLen(ctx, "events:dlq").Result()
	if err == nil {
		s.dlqDepth.Set(float64(dlqLen))
	}
}
