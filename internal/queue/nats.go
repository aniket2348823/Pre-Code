package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/vigilagent/vigilagent/internal/config"
)

// NATS holds the NATS connection and JetStream context.
type NATS struct {
	Conn *nats.Conn
	JS   jetstream.JetStream
}

// NewNATS creates a new NATS connection and initializes JetStream.
func NewNATS(cfg *config.NATSConfig) (*NATS, error) {
	// Fail fast on bad config instead of relying on nats.Connect defaults:
	// nats.Connect("") silently falls back to nats://127.0.0.1:4222 and, with
	// RetryOnFailedConnect, never errors — an empty URL would silently talk to
	// localhost. The same applies to a blank stream name, which JetStream
	// would otherwise reject only at stream-creation time.
	if cfg == nil {
		return nil, fmt.Errorf("nats config is required")
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("nats url is required")
	}
	if strings.TrimSpace(cfg.Stream) == "" {
		return nil, fmt.Errorf("nats stream name is required")
	}

	nc, err := nats.Connect(cfg.URL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			slog.Warn("nats disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			slog.Info("nats reconnected", "url", nc.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nats: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create jetstream context: %w", err)
	}

	// Ensure the stream exists
	// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
	ctx := context.Background()
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     cfg.Stream,
		Subjects: []string{cfg.Stream + ".>"},
		Storage:  jetstream.FileStorage,
		MaxMsgs:  1_000_000,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create/update jetstream stream: %w", err)
	}

	slog.Info("connected to nats", "url", cfg.URL, "stream", cfg.Stream)

	return &NATS{
		Conn: nc,
		JS:   js,
	}, nil
}

// HealthCheck verifies the NATS connection is alive.
func (n *NATS) HealthCheck() error {
	if !n.Conn.IsConnected() {
		return fmt.Errorf("nats not connected")
	}
	return nil
}

// Drain gracefully drains in-flight messages before closing.
func (n *NATS) Drain(ctx context.Context) error {
	if n.Conn == nil {
		return nil
	}
	slog.Info("nats: draining in-flight messages")
	// nats.Conn.Drain() is synchronous but we respect the context for timeout
	done := make(chan error, 1)
	go func() {
		done <- n.Conn.Drain()
	}()
	select {
	case err := <-done:
		if err != nil {
			slog.Warn("nats drain failed", "error", err)
		}
		return err
	case <-ctx.Done():
		slog.Warn("nats drain timed out, forcing close")
		n.Conn.Close()
		return ctx.Err()
	}
}

// Close forcefully closes the NATS connection.
func (n *NATS) Close() {
	if n.Conn != nil {
		n.Conn.Close()
		slog.Info("nats connection closed")
	}
}
