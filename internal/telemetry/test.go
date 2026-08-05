package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestMetricsHandler_BeforeSetup(t *testing.T) {
	metricsHandlerMu.Lock()
	metricsHandler = nil
	metricsHandlerMu.Unlock()
	setupDone.Store(false)
	h := MetricsHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestMetricsHandler_AfterSetup(t *testing.T) {
	metricsHandlerMu.Lock()
	metricsHandler = nil
	metricsHandlerMu.Unlock()
	setupDone.Store(false)
	ctx := context.Background()
	cleanup, err := Setup(ctx, "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer cleanup(context.Background())

	h := MetricsHandler()
	if h == nil {
		t.Fatal("expected non-nil handler after setup")
	}
}

func TestSetup_Cleanup(t *testing.T) {
	ctx := context.Background()
	cleanup, err := Setup(ctx, "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	if err := cleanup(context.Background()); err != nil {
		t.Errorf("cleanup returned error: %v", err)
	}
}

func TestSetup_DuplicateCalls(t *testing.T) {
	ctx := context.Background()
	cleanup1, err := Setup(ctx, "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("first Setup failed: %v", err)
	}
	defer cleanup1(context.Background())

	cleanup2, err := Setup(ctx, "test-service", "0.0.2")
	if err != nil {
		t.Fatalf("second Setup failed: %v", err)
	}
	defer cleanup2(context.Background())
}

func TestSetup_CleanupErrors(t *testing.T) {
	ctx := context.Background()
	cleanup, err := Setup(ctx, "test-service", "0.0.1")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	expiredCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	cleanup(expiredCtx)
}

func TestSetup_ResourceError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Setup(ctx, "test-service", "0.0.1")
	if err == nil {
		return
	}
}
