package router

import (
	"net/http"
	"time"

	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/idempotency"
	"github.com/vigilagent/vigilagent/internal/rateguard"
	"github.com/vigilagent/vigilagent/internal/requestid"
	"github.com/vigilagent/vigilagent/internal/signing"
)

// MiddlewareConfig holds configuration for the full middleware stack.
type MiddlewareConfig struct {
	Signing       *signing.Signer
	IPFilter      interface{ Middleware(http.Handler) http.Handler }
	CORS          *cors.Config
	RequestID     bool
	Timeout       time.Duration
	Idempotency   *idempotency.Store
	RateGuard     *rateguard.EndpointLimiter
}

// setupSecurityMiddleware applies security-focused middleware: request signing,
// IP filtering, and CORS. These run before business logic.
func (r *Router) setupSecurityMiddleware(cfg *MiddlewareConfig) {
	// 1. Request signing verification (if configured)
	if cfg != nil && cfg.Signing != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				// Skip signing for health, metrics, and OPTIONS
				if req.URL.Path == "/api/v1/health" ||
					req.URL.Path == "/api/v1/metrics" ||
					req.Method == http.MethodOptions {
					next.ServeHTTP(w, req)
					return
				}
				if err := cfg.Signing.VerifyRequest(req, nil); err != nil {
					http.Error(w, "invalid signature", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, req)
			})
		})
	}

	// 2. IP filtering (if configured)
	if cfg != nil && cfg.IPFilter != nil {
		r.Use(cfg.IPFilter.Middleware)
	}

	// 3. CORS (use dedicated package if configured, else fallback to inline)
	if cfg != nil && cfg.CORS != nil {
		r.Use(cfg.CORS.Middleware)
	}
}

// setupResilienceMiddleware applies resilience-focused middleware: per-endpoint
// rate limiting, idempotency protection, and request timeout.
func (r *Router) setupResilienceMiddleware(cfg *MiddlewareConfig) {
	// 1. Per-endpoint rate limiting
	if cfg != nil && cfg.RateGuard != nil {
		r.Use(cfg.RateGuard.Middleware)
	}

	// 2. Idempotency protection
	if cfg != nil && cfg.Idempotency != nil {
		r.Use(cfg.Idempotency.Middleware)
	}

	// 3. Request timeout (override chi's default if configured)
	if cfg != nil && cfg.Timeout > 0 {
		r.Use(func(next http.Handler) http.Handler {
			return http.TimeoutHandler(next, cfg.Timeout, `{"error":"request timeout"}`)
		})
	}
}

// setupObservabilityMiddleware applies observability-focused middleware:
// request ID propagation and structured logging.
func (r *Router) setupObservabilityMiddleware(cfg *MiddlewareConfig) {
	// 1. Request ID (use dedicated package if configured)
	if cfg != nil && cfg.RequestID {
		r.Use(requestid.Middleware)
	}
}

// NewWithMiddleware creates a Router with the full middleware stack wired.
// Unlike New(), it replaces the default middleware with the provided config.
// All middleware is wired BEFORE routes are set up, as required by chi.
func NewWithMiddleware(opts Options, mcfg *MiddlewareConfig) *Router {
	r := newRouter(opts)

	// Build handlers using shared logic.
	r.initHandlers()

	// Wire security middleware first (outermost)
	r.setupSecurityMiddleware(mcfg)

	// Wire resilience middleware
	r.setupResilienceMiddleware(mcfg)

	// Wire observability middleware
	r.setupObservabilityMiddleware(mcfg)

	// Routes must come LAST (after all middleware).
	r.setupRoutes()

	return r
}
