// Package graceful provides graceful HTTP server shutdown with configurable timeout.
package graceful

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Server wraps an HTTP server with graceful shutdown support.
type Server struct {
	httpServer *http.Server
	timeout    time.Duration
}

// New creates a new graceful server with the given handler and timeout.
func New(handler http.Handler, addr string, timeout time.Duration) *Server {
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	return &Server{
		httpServer: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
		timeout: timeout,
	}
}

// ListenAndServe starts the server and blocks until shutdown signal.
func (s *Server) ListenAndServe() error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		slog.Info("shutdown signal received", "signal", sig)
	}

	return s.Shutdown()
}

// Shutdown gracefully shuts down the server with a timeout.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	slog.Info("shutting down server", "timeout", s.timeout)
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	slog.Info("server shut down gracefully")
	return nil
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}
