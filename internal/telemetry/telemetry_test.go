package telemetry

import (
	"context"
	"testing"
)

func TestSetupReturnsCleanup(t *testing.T) {
	cleanup, err := Setup(context.Background(), "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup function")
	}
	// Cleanup should not panic
	cleanup()
}

func TestMetricsHandlerNotNil(t *testing.T) {
	// Setup initializes the global metrics handler
	_, err := Setup(context.Background(), "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	h := MetricsHandler()
	if h == nil {
		t.Fatal("MetricsHandler returned nil after Setup")
	}
}

func TestSetupIdempotent(t *testing.T) {
	// Calling Setup twice should not panic (idempotent)
	cleanup1, err := Setup(context.Background(), "test-service-1", "0.0.1")
	if err != nil {
		t.Fatalf("first Setup failed: %v", err)
	}
	defer cleanup1()

	cleanup2, err := Setup(context.Background(), "test-service-2", "0.0.2")
	if err != nil {
		t.Fatalf("second Setup failed: %v", err)
	}
	defer cleanup2()
}
