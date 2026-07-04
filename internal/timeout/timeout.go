// Package timeout provides HTTP request timeout middleware.
package timeout

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Middleware wraps an HTTP handler with a request timeout.
// If the handler doesn't complete within the duration, the context is cancelled
// and a 504 Gateway Timeout is returned.
func Middleware(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			done := make(chan struct{})
			ww := &timeoutWriter{ResponseWriter: w, done: done}

			go func() {
				next.ServeHTTP(ww, r.WithContext(ctx))
				close(done)
			}()

			select {
			case <-done:
				return
			case <-ctx.Done():
				ww.mu.Lock()
				defer ww.mu.Unlock()
				if !ww.wroteHeader {
					http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
					ww.wroteHeader = true
				}
			}
		})
	}
}

// timeoutWriter tracks whether a response has been written.
type timeoutWriter struct {
	http.ResponseWriter
	mu          sync.Mutex
	wroteHeader bool
	done        chan struct{}
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.wroteHeader = true
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.wroteHeader = true
	return tw.ResponseWriter.Write(b)
}
