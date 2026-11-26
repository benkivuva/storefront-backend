package middleware

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

// Tracing extracts W3C trace context, starts a server span, and stores the
// incoming traceparent on the request context so the outbox can persist it for
// downstream consumers. A no-op provider is used unless the process installs
// one, so this is safe in local development and tests.
func Tracing() func(http.Handler) http.Handler {
	propagator := propagation.TraceContext{}
	tracer := otel.Tracer("storefront-backend/http")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			spanName := r.Method + " " + r.URL.Path
			ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			if tp := r.Header.Get("traceparent"); tp != "" {
				ctx = context.WithValue(ctx, domain.TraceparentKey, tp)
			} else if sc := span.SpanContext(); sc.IsValid() {
				tp := "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-01"
				ctx = context.WithValue(ctx, domain.TraceparentKey, tp)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
