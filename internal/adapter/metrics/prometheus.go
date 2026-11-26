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

// Streams is the set of Redis streams whose depth and pending-entry count are
// exported. Keep in sync with the relay/consumer stream naming.
var Streams = []string{"events:order", "events:product", "events:payment", "events:dlq"}

type Server struct {
	outbox domain.OutboxRepository
	rdb    *goredis.Client
	addr   string

	outboxOldestPending prometheus.Gauge
	dlqDepth            prometheus.Gauge
	streamLength        *prometheus.GaugeVec
	streamPending       *prometheus.GaugeVec
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
		streamLength: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "redis_stream_length",
			Help: "Number of entries in a Redis stream",
		}, []string{"stream"}),
		streamPending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "redis_stream_pending",
			Help: "Number of pending (unacked) entries in a Redis consumer group",
		}, []string{"stream", "group"}),
	}
	prometheus.MustRegister(s.outboxOldestPending, s.dlqDepth, s.streamLength, s.streamPending)
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

	for _, stream := range Streams {
		if length, err := s.rdb.XLen(ctx, stream).Result(); err == nil {
			s.streamLength.WithLabelValues(stream).Set(float64(length))
		}
		if stream == "events:dlq" {
			if length, err := s.rdb.XLen(ctx, stream).Result(); err == nil {
				s.dlqDepth.Set(float64(length))
			}
			continue
		}
		pending, err := s.rdb.XPending(ctx, stream, "email-group").Result()
		if err == nil && pending != nil {
			s.streamPending.WithLabelValues(stream, "email-group").Set(float64(pending.Count))
		}
	}
}

// CacheObserver exports cache hit/miss counters so the cache-hit-ratio
// dashboard has a real signal.
type CacheObserver struct {
	hits   prometheus.Counter
	misses prometheus.Counter
}

func NewCacheObserver() *CacheObserver {
	o := &CacheObserver{
		hits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "Number of cache lookups served from Redis",
		}),
		misses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cache_misses_total",
			Help: "Number of cache lookups that required a database fetch",
		}),
	}
	for _, c := range []prometheus.Collector{o.hits, o.misses} {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				slog.Warn("cache metric registration failed", "error", err)
			}
		}
	}
	return o
}

func (o *CacheObserver) Hit()  { o.hits.Inc() }
func (o *CacheObserver) Miss() { o.misses.Inc() }
