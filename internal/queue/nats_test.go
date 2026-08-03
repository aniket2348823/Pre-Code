package queue

import (
	"context"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/config"
)

// --- NATS struct ---

func TestNATS_StructZeroValues(t *testing.T) {
	n := &NATS{}
	if n.Conn != nil {
		t.Fatal("expected nil Conn for zero struct")
	}
	if n.JS != nil {
		t.Fatal("expected nil JS for zero struct")
	}
}

// --- HealthCheck edge cases ---

func TestNATS_HealthCheck_NilConnPanics(t *testing.T) {
	n := &NATS{Conn: nil}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil conn")
			}
		}()
		n.HealthCheck()
	}()
}

func TestNATS_HealthCheck_Disconnected(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	defer nc.Close()
	n := &NATS{Conn: nc}
	err := n.HealthCheck()
	if err == nil {
		t.Error("expected error for disconnected NATS")
	}
}

func TestNATS_HealthCheck_AfterClose(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	n := &NATS{Conn: nc}
	n.Close()
	err := n.HealthCheck()
	if err == nil {
		t.Error("expected error after close")
	}
}

// --- Drain edge cases ---

func TestNATS_Drain_NilConn(t *testing.T) {
	n := &NATS{Conn: nil}
	err := n.Drain(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for nil conn, got %v", err)
	}
}

func TestNATS_Drain_ContextTimeout(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	defer nc.Close()
	n := &NATS{Conn: nc}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := n.Drain(ctx)
	if err == nil {
		t.Error("expected timeout error from drain")
	}
}

func TestNATS_Drain_ContextCancelled(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	defer nc.Close()
	n := &NATS{Conn: nc}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := n.Drain(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// --- Close edge cases ---

func TestNATS_Close_NilConnNoPanic(t *testing.T) {
	n := &NATS{Conn: nil}
	n.Close()
}

func TestNATS_Close_DisconnectedConn(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	n := &NATS{Conn: nc}
	n.Close()
}

func TestNATS_Close_CalledTwice(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	n := &NATS{Conn: nc}
	n.Close()
	n.Close() // should not panic
}

// --- NewNATS config ---

func TestNewNATS_UnreachableServer(t *testing.T) {
	cfg := &config.NATSConfig{
		URL:    "nats://127.0.0.1:1",
		Stream: "test-stream",
	}
	n, err := NewNATS(cfg)
	if err != nil {
		t.Logf("NewNATS failed as expected: %v", err)
		return
	}
	if n != nil {
		n.Close()
		t.Error("expected error from NewNATS with unreachable server")
	}
}

func TestNewNATS_EmptyURL(t *testing.T) {
	cfg := &config.NATSConfig{
		URL:    "",
		Stream: "test-stream",
	}
	_, err := NewNATS(cfg)
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestNewNATS_EmptyStream(t *testing.T) {
	cfg := &config.NATSConfig{
		URL:    "nats://127.0.0.1:4222",
		Stream: "",
	}
	_, err := NewNATS(cfg)
	if err == nil {
		t.Fatal("expected error for empty stream")
	}
}

// --- Drain success path ---

func TestNATS_Drain_Success(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	n := &NATS{Conn: nc}
	err := n.Drain(context.Background())
	if err != nil {
		t.Logf("drain returned error (expected for disconnected): %v", err)
	}
}

// --- Drain with very short timeout ---

func TestNATS_Drain_ShortTimeout(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	defer nc.Close()
	n := &NATS{Conn: nc}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	err := n.Drain(ctx)
	if err == nil {
		t.Error("expected error from expired timeout")
	}
}
