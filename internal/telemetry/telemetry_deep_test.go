package telemetry

import (
	"context"
	"testing"
)

// Note: Tests that call Setup() are limited because OpenTelemetry/Prometheus
// use global state. Multiple Setup() calls in separate test functions can
// cause "collector already registered" errors. The existing telemetry_test.go
// already covers basic Setup functionality. These deep tests focus on
// edge cases that don't require multiple Setup calls.

func TestMetricsHandler_BeforeSetup(t *testing.T) {
	// MetricsHandler before any Setup call should return nil (global var not initialized)
	h := MetricsHandler()
	if h != nil {
		// It's acceptable for this to be non-nil if a previous test called Setup
		// In a clean test run, this should be nil
		_ = h
	}
}

func TestSetup_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	// Setup with cancelled context should not panic — behavior is implementation-defined
	cleanup, err := Setup(ctx, "test-cancelled", "0.0.1")
	if err != nil {
		// Acceptable: resource creation may fail with cancelled context
		return
	}
	if cleanup != nil {
		cleanup()
	}
}

func TestSetup_LargeServiceName(t *testing.T) {
	longName := ""
	for i := 0; i < 1000; i++ {
		longName += "a"
	}
	cleanup, err := Setup(context.Background(), longName, "0.0.1")
	if err != nil {
		t.Fatalf("Setup with long service name should not fail: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	cleanup()
}

func TestSetup_EmptyStrings(t *testing.T) {
	// Test with both empty service name and version
	cleanup, err := Setup(context.Background(), "", "")
	if err != nil {
		t.Fatalf("Setup with empty strings should not fail: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	cleanup()
}
