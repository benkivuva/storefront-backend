package httpsrv

import (
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/handlers"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

const (
	authBodyLimit    = 64 << 10
	defaultBodyLimit = 1 << 20
)

type RouterConfig struct {
	AuthHandler    *handlers.AuthHandler
	MeHandler      *handlers.MeHandler
	CatalogHandler *handlers.CatalogHandler
	JWTVerifier    middleware.TokenVerifier
	AuthLimiter    middleware.RateLimiter
	AuthFallback   middleware.RateLimiter
	GlobalLimiter  middleware.RateLimiter
	GlobalFallback middleware.RateLimiter
	TrustedProxies []netip.Prefix
	AllowedOrigins []string
	AuthCapacity   int
	GlobalCapacity int
	Prod           bool
}

func NewPublicRouter(cfg RouterConfig) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID())
	r.Use(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{Prod: cfg.Prod}))
	r.Use(middleware.CORS(cfg.AllowedOrigins, true))
	r.Use(middleware.BodyLimit(defaultBodyLimit))

	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Use(middleware.BodyLimit(authBodyLimit))
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:  cfg.AuthLimiter,
			Fallback: cfg.AuthFallback,
			Prefix:   "rl:auth",
			Capacity: cfg.AuthCapacity,
			Key:      middleware.AccountIPKey(cfg.TrustedProxies),
		}))
		r.Post("/register", cfg.AuthHandler.Register)
		r.Post("/login", cfg.AuthHandler.Login)
		r.Group(func(r chi.Router) {
			r.Use(middleware.OriginCheck(cfg.AllowedOrigins))
			r.Post("/refresh", cfg.AuthHandler.Refresh)
			r.Post("/logout", cfg.AuthHandler.Logout)
		})
	})

	r.Route("/api/v1/me", func(r chi.Router) {
		r.Use(middleware.Auth(cfg.JWTVerifier))
		r.Use(middleware.RequireRole())
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:  cfg.GlobalLimiter,
			Fallback: cfg.GlobalFallback,
			Prefix:   "rl:global",
			Capacity: cfg.GlobalCapacity,
			Key:      middleware.AccountIPKey(cfg.TrustedProxies),
		}))
		r.Get("/", cfg.MeHandler.GetMe)
		r.Patch("/", cfg.MeHandler.UpdateMe)
		r.Get("/addresses", cfg.MeHandler.ListAddresses)
		r.Post("/addresses", cfg.MeHandler.CreateAddress)
		r.Get("/sessions", cfg.MeHandler.ListSessions)
		r.Delete("/sessions/{id}", cfg.MeHandler.RevokeSession)
	})

	r.Route("/api/v1/products", func(r chi.Router) {
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:  cfg.GlobalLimiter,
			Fallback: cfg.GlobalFallback,
			Prefix:   "rl:catalog",
			Capacity: cfg.GlobalCapacity,
			Key:      middleware.IPKey(cfg.TrustedProxies),
		}))
		r.Get("/", cfg.CatalogHandler.ListProducts)
		r.Get("/{id}", cfg.CatalogHandler.GetProduct)
	})

	r.Route("/api/v1/categories", func(r chi.Router) {
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:  cfg.GlobalLimiter,
			Fallback: cfg.GlobalFallback,
			Prefix:   "rl:catalog",
			Capacity: cfg.GlobalCapacity,
			Key:      middleware.IPKey(cfg.TrustedProxies),
		}))
		r.Get("/", cfg.CatalogHandler.ListCategories)
	})

	r.Route("/api/v1/admin/products", func(r chi.Router) {
		r.Use(middleware.Auth(cfg.JWTVerifier))
		r.Use(middleware.RequireRole(domain.RoleAdmin))
		r.Post("/", cfg.CatalogHandler.CreateProduct)
		r.Put("/{id}", cfg.CatalogHandler.UpdateProduct)
		r.Delete("/{id}", cfg.CatalogHandler.DeleteProduct)
		r.Post("/{id}/inventory", cfg.CatalogHandler.AdjustStock)
		r.Get("/low-stock", cfg.CatalogHandler.ListLowStock)
	})

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		dto.WriteProblem(w, http.StatusNotFound, "NOT_FOUND", "not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		dto.WriteProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	})
	return r
}

func NewInternalRouter(ready *Ready) chi.Router {
	r := chi.NewRouter()
	r.Method(http.MethodGet, "/healthz", HealthHandler())
	r.Method(http.MethodGet, "/readyz", ready.Handler())
	r.Method(http.MethodGet, "/metrics", promhttp.Handler())

	r.Get("/api/v1/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "api/openapi.yaml")
	})
	r.Get("/api/v1/docs", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "api/docs.html")
	})
	return r
}
