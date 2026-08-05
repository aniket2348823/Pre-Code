package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/vigilagent/vigilagent/internal/config"
)

func CORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	allowedMethods := strings.ToUpper(strings.Join(cfg.AllowedMethods, ", "))
	allowedHeaders := strings.Join(cfg.AllowedHeaders, ",")
	maxAge := strconv.Itoa(cfg.MaxAge)

	originSet := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		originSet[strings.ToLower(o)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := false
			originLower := strings.ToLower(origin)
			if originSet["*"] {
				allowed = true
			} else if originSet[originLower] {
				allowed = true
			} else {
				for pattern := range originSet {
					if strings.HasPrefix(pattern, "*.") {
						suffix := pattern[1:] // ".example.com"
						if strings.HasSuffix(originLower, suffix) {
							allowed = true
							break
						}
					}
				}
			}

			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			// Preflight
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
				w.Header().Set("Access-Control-Max-Age", maxAge)
				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Simple + actual request.
			// When credentials are allowed, the spec requires echoing the specific
			// origin (wildcard "*" is rejected by browsers and unsafe with credentials).
			if originSet["*"] && !cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			next.ServeHTTP(w, r)
		})
	}
}
