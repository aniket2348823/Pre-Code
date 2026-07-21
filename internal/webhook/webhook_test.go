package webhook

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	return pool
}

func TestNewEngine(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()
	e := NewEngine(pool)
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
}

func TestRegisterAndList(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()
	e := NewEngine(pool)

	userID := "test-user-1"
	ep := &Endpoint{
		UserID: userID,
		URL:    "https://example.com/webhook",
		Events: []string{"scan.completed"},
		Active: true,
	}
	if err := e.Register(ctx, ep); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	defer e.Unregister(ctx, userID, ep.ID)

	eps, err := e.ListEndpoints(ctx, userID)
	if err != nil {
		t.Fatalf("ListEndpoints failed: %v", err)
	}
	found := false
	for _, got := range eps {
		if got.ID == ep.ID {
			found = true
			if got.URL != "https://example.com/webhook" {
				t.Errorf("expected URL https://example.com/webhook, got %s", got.URL)
			}
			if !got.Active {
				t.Error("expected endpoint to be active")
			}
		}
	}
	if !found {
		t.Error("registered endpoint not found in list")
	}
}

func TestUnregister(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()
	e := NewEngine(pool)

	userID := "test-user-2"
	ep := &Endpoint{UserID: userID, URL: "https://example.com", Active: true}
	_ = e.Register(ctx, ep)

	if err := e.Unregister(ctx, userID, ep.ID); err != nil {
		t.Errorf("expected unregister to succeed: %v", err)
	}
}

func TestGetEndpoint(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()
	e := NewEngine(pool)

	userID := "test-user-3"
	ep := &Endpoint{UserID: userID, URL: "https://example.com", Secret: "s3cret", Active: true}
	_ = e.Register(ctx, ep)
	defer e.Unregister(ctx, userID, ep.ID)

	got, err := e.GetEndpoint(ctx, userID, ep.ID)
	if err != nil {
		t.Fatalf("GetEndpoint failed: %v", err)
	}
	if got.URL != "https://example.com" {
		t.Errorf("expected URL https://example.com, got %s", got.URL)
	}
}

func TestComputeAndVerifySignature(t *testing.T) {
	secret := "my-secret-key"
	payload := []byte(`{"event":"test"}`)
	sig := ComputeSignature(secret, payload)
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if !VerifySignature([]byte(secret), payload, sig) {
		t.Error("expected signature to verify")
	}
	if VerifySignature([]byte("wrong-secret"), payload, sig) {
		t.Error("signature should not verify with wrong secret")
	}
}

func TestDispatchNoEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()
	e := NewEngine(pool)
	// Should not panic
	e.Dispatch(ctx, Event{ID: "ev1", Type: "test.event"})
}

func TestDispatchMatchingEndpoint(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()
	e := NewEngine(pool)

	userID := "test-user-4"
	ep := &Endpoint{
		UserID: userID,
		URL:    "http://localhost:99999/webhook", // will fail but shouldn't panic
		Events: []string{"test.event"},
		Active: true,
	}
	_ = e.Register(ctx, ep)
	defer e.Unregister(ctx, userID, ep.ID)

	e.Dispatch(ctx, Event{ID: "ev1", Type: "test.event"})
	time.Sleep(200 * time.Millisecond) // give goroutine time to run
}

func TestStats(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()
	e := NewEngine(pool)

	userID := "test-user-5"
	stats, err := e.Stats(ctx, userID)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats["endpoints"] == nil {
		t.Error("expected endpoints key in stats")
	}
}
