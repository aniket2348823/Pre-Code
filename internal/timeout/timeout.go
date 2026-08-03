package timeout

import (
	"context"
	"net/http"
	"sync"
	"time"
)

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
				ww.timedOut = true
				if !ww.wroteHeader {
					w.WriteHeader(http.StatusGatewayTimeout)
					ww.wroteHeader = true
				}
			}
		})
	}
}

type timeoutWriter struct {
	http.ResponseWriter
	mu          sync.Mutex
	wroteHeader bool
	timedOut    bool
	done        chan struct{}
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.wroteHeader = true
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	tw.wroteHeader = true
	return tw.ResponseWriter.Write(b)
}
