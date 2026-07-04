package webhook

import (
	"context"
	"testing"
	"time"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
}

func TestRegisterAndList(t *testing.T) {
	e := NewEngine()
	e.Register(&Endpoint{
		ID:     "ep1",
		URL:    "https://example.com/webhook",
		Events: []string{"scan.completed"},
		Active: true,
	})
	eps := e.ListEndpoints()
	if len(eps) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(eps))
	}
	if eps[0].ID != "ep1" {
		t.Errorf("expected ep1, got %s", eps[0].ID)
	}
	if !eps[0].Active {
		t.Error("expected endpoint to be active")
	}
}

func TestUnregister(t *testing.T) {
	e := NewEngine()
	e.Register(&Endpoint{ID: "ep1", URL: "https://example.com"})
	if !e.Unregister("ep1") {
		t.Error("expected unregister to return true")
	}
	if e.Unregister("ep1") {
		t.Error("expected unregister to return false for nonexistent")
	}
}

func TestGetEndpoint(t *testing.T) {
	e := NewEngine()
	e.Register(&Endpoint{ID: "ep1", URL: "https://example.com", Secret: "s3cret"})
	ep := e.GetEndpoint("ep1")
	if ep == nil {
		t.Fatal("expected endpoint")
	}
	if ep.URL != "https://example.com" {
		t.Errorf("expected URL, got %s", ep.URL)
	}
	if e.GetEndpoint("nonexistent") != nil {
		t.Error("expected nil for nonexistent")
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
	e := NewEngine()
	// Should not panic
	e.Dispatch(context.Background(), Event{
		ID:   "ev1",
		Type: "test.event",
	})
}

func TestDispatchMatchingEndpoint(t *testing.T) {
	e := NewEngine()
	e.Register(&Endpoint{
		ID:     "ep1",
		URL:    "http://localhost:99999/webhook", // will fail but shouldn't panic
		Events: []string{"test.event"},
	})
	e.Dispatch(context.Background(), Event{
		ID:   "ev1",
		Type: "test.event",
	})
	// Give goroutine time to run
	time.Sleep(100 * time.Millisecond)
}

func TestDispatchWildcardSubscription(t *testing.T) {
	e := NewEngine()
	e.Register(&Endpoint{
		ID:     "ep1",
		URL:    "http://localhost:99999/webhook",
		Events: []string{"*"},
	})
	e.Dispatch(context.Background(), Event{
		ID:   "ev1",
		Type: "any.event",
	})
	time.Sleep(100 * time.Millisecond)
}

func TestDispatchInactiveEndpoint(t *testing.T) {
	e := NewEngine()
	e.Register(&Endpoint{
		ID:     "ep1",
		URL:    "http://localhost:99999/webhook",
		Events: []string{"test.event"},
		Active: false,
	})
	e.Dispatch(context.Background(), Event{
		ID:   "ev1",
		Type: "test.event",
	})
	time.Sleep(100 * time.Millisecond)
	// Should have no delivery results since endpoint was inactive
	results := e.GetResults(10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for inactive endpoint, got %d", len(results))
	}
}

func TestStats(t *testing.T) {
	e := NewEngine()
	stats := e.Stats()
	if stats["endpoints"] != 0 {
		t.Error("expected 0 endpoints")
	}
	if stats["total_deliveries"] != 0 {
		t.Error("expected 0 deliveries")
	}
}
