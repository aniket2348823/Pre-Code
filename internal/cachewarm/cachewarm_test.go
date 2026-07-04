package cachewarm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegisterAndGet(t *testing.T) {
	w := NewWarmer()
	w.Register("test-key", time.Hour, func(ctx context.Context) (interface{}, error) {
		return "test-value", nil
	})

	entry, ok := w.Get("test-key")
	if !ok {
		t.Fatal("expected entry to exist")
	}
	if entry.Value != nil {
		t.Fatal("value should be nil before first run")
	}
}

func TestExecuteJob(t *testing.T) {
	w := NewWarmer()
	w.Register("compute", time.Hour, func(ctx context.Context) (interface{}, error) {
		return 42, nil
	})

	w.mu.Lock()
	j := w.jobs["compute"]
	w.mu.Unlock()

	w.executeJob(context.Background(), j)

	entry, ok := w.Get("compute")
	if !ok {
		t.Fatal("expected entry")
	}
	if entry.Value != 42 {
		t.Fatalf("expected 42, got %v", entry.Value)
	}
	if entry.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestExecuteJobError(t *testing.T) {
	w := NewWarmer()
	w.Register("fail", time.Hour, func(ctx context.Context) (interface{}, error) {
		return nil, context.DeadlineExceeded
	})

	w.mu.Lock()
	j := w.jobs["fail"]
	w.mu.Unlock()

	w.executeJob(context.Background(), j)

	entry, ok := w.Get("fail")
	if !ok {
		t.Fatal("expected entry")
	}
	if entry.Err != context.DeadlineExceeded {
		t.Fatalf("expected deadline exceeded error, got %v", entry.Err)
	}
}

func TestStartRunsJobs(t *testing.T) {
	w := NewWarmer()
	var count int64
	w.Register("counter", time.Hour, func(ctx context.Context) (interface{}, error) {
		atomic.AddInt64(&count, 1)
		return atomic.LoadInt64(&count), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	entry, ok := w.Get("counter")
	if !ok {
		t.Fatal("expected entry")
	}
	if entry.Value != int64(1) {
		t.Fatalf("expected value 1, got %v", entry.Value)
	}
}

func TestStop(t *testing.T) {
	w := NewWarmer()
	w.Register("key1", time.Hour, func(ctx context.Context) (interface{}, error) {
		return "val", nil
	})

	w.Stop()
	w.Stop() // double stop should not panic

	if len(w.Keys()) != 1 {
		t.Fatal("keys should still be accessible after stop")
	}
}

func TestGetUnknownKey(t *testing.T) {
	w := NewWarmer()
	_, ok := w.Get("nonexistent")
	if ok {
		t.Fatal("expected false for unknown key")
	}
}

func TestKeys(t *testing.T) {
	w := NewWarmer()
	w.Register("a", time.Hour, func(ctx context.Context) (interface{}, error) { return nil, nil })
	w.Register("b", time.Hour, func(ctx context.Context) (interface{}, error) { return nil, nil })

	keys := w.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}
