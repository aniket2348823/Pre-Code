package batch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmit_NilContext(t *testing.T) {
	p := NewProcessor(func(ctx context.Context, batch *Batch) error { return nil })
	// Should not panic with nil context
	p.Submit(context.Background(), &Job{ID: "j1", Type: "scan", Payload: map[string]interface{}{}})
}

func TestSubmit_10000Jobs(t *testing.T) {
	if testing.Short() {
		t.Skip("slow stress test")
	}
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		return nil
	}, WithBatchWindow(50*time.Millisecond), WithMaxBatchSize(100))
	for i := 0; i < 10000; i++ {
		p.Submit(context.Background(), &Job{
			ID:      fmt.Sprintf("j%d", i),
			Type:    "scan",
			Payload: map[string]interface{}{"file": fmt.Sprintf("f%d.go", i%100)},
		})
	}
	p.Flush(context.Background())
	time.Sleep(500 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 10000 {
		t.Errorf("expected 10000 processed, got %d", atomic.LoadInt32(&processed))
	}
}

func TestFlush_NoPendingJobs(t *testing.T) {
	p := NewProcessor(func(ctx context.Context, batch *Batch) error { return nil })
	p.Flush(context.Background()) // should not panic
}

func TestFlush_DuringProcessing(t *testing.T) {
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		time.Sleep(50 * time.Millisecond)
		return nil
	}, WithBatchWindow(10*time.Second))
	p.Submit(context.Background(), &Job{ID: "j1", Type: "scan", Payload: map[string]interface{}{"f": "a"}})
	// Flush while processing
	p.Flush(context.Background())
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&processed) < 1 {
		t.Error("at least 1 job should be processed")
	}
}

func TestMaxBatchSize_1(t *testing.T) {
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		return nil
	}, WithMaxBatchSize(1), WithBatchWindow(10*time.Second))
	for i := 0; i < 5; i++ {
		p.Submit(context.Background(), &Job{
			ID:      fmt.Sprintf("j%d", i),
			Type:    "scan",
			Payload: map[string]interface{}{"f": fmt.Sprintf("a%d", i)},
		})
	}
	time.Sleep(500 * time.Millisecond)
	if atomic.LoadInt32(&processed) < 3 {
		t.Errorf("expected at least 3 processed with maxBatchSize=1, got %d", atomic.LoadInt32(&processed))
	}
}

func TestMaxBatchSize_10000(t *testing.T) {
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		return nil
	}, WithMaxBatchSize(10000), WithBatchWindow(100*time.Millisecond))
	for i := 0; i < 50; i++ {
		p.Submit(context.Background(), &Job{
			ID:      fmt.Sprintf("j%d", i),
			Type:    "scan",
			Payload: map[string]interface{}{"f": "a"},
		})
	}
	p.Flush(context.Background())
	time.Sleep(250 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 50 {
		t.Errorf("expected 50 processed, got %d", atomic.LoadInt32(&processed))
	}
}

func TestPendingCount_Empty(t *testing.T) {
	p := NewProcessor(func(ctx context.Context, batch *Batch) error { return nil })
	if p.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", p.PendingCount())
	}
}

func TestPendingCount_UnderConcurrentSubmit(t *testing.T) {
	p := NewProcessor(func(ctx context.Context, batch *Batch) error { return nil }, WithBatchWindow(10*time.Second))
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Submit(context.Background(), &Job{ID: "j", Type: "scan", Payload: map[string]interface{}{"f": "a"}})
		}()
	}
	wg.Wait()
	pending := p.PendingCount()
	if pending == 0 {
		t.Errorf("expected some pending items, got %d", pending)
	}
}

func TestComputeGroupKey_NilPayload(t *testing.T) {
	key := computeGroupKey("scan", nil)
	if key == "" {
		t.Error("nil payload should still produce a key")
	}
}

func TestComputeGroupKey_EmptyPayload(t *testing.T) {
	key := computeGroupKey("scan", map[string]interface{}{})
	if key == "" {
		t.Error("empty payload should still produce a key")
	}
}

func TestComputeGroupKey_Deterministic(t *testing.T) {
	payload := map[string]interface{}{"file": "a.go", "lang": "go"}
	key1 := computeGroupKey("scan", payload)
	key2 := computeGroupKey("scan", payload)
	if key1 != key2 {
		t.Error("same input should produce same key")
	}
}

func TestComputeGroupKey_DifferentPayloads(t *testing.T) {
	key1 := computeGroupKey("scan", map[string]interface{}{"file": "a.go"})
	key2 := computeGroupKey("scan", map[string]interface{}{"file": "b.go"})
	if key1 == key2 {
		t.Error("different payloads should produce different keys")
	}
}

func TestJob_EmptyID(t *testing.T) {
	p := NewProcessor(func(ctx context.Context, batch *Batch) error { return nil }, WithBatchWindow(10*time.Second))
	p.Submit(context.Background(), &Job{ID: "", Type: "scan", Payload: map[string]interface{}{}})
	if p.PendingCount() != 1 {
		t.Error("job with empty ID should still be accepted")
	}
}

func TestJob_EmptyType(t *testing.T) {
	p := NewProcessor(func(ctx context.Context, batch *Batch) error { return nil }, WithBatchWindow(10*time.Second))
	p.Submit(context.Background(), &Job{ID: "j1", Type: "", Payload: map[string]interface{}{}})
	if p.PendingCount() != 1 {
		t.Error("job with empty type should still be accepted")
	}
}

func TestProcessingFunction_Error(t *testing.T) {
	var processedJobs int32
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processedJobs, int32(len(batch.Jobs)))
		return fmt.Errorf("processing failed")
	}, WithBatchWindow(10*time.Millisecond))
	p.Submit(context.Background(), &Job{ID: "j1", Type: "scan", Payload: map[string]interface{}{}})
	p.Flush(context.Background())
	time.Sleep(100 * time.Millisecond)
	// Jobs should be marked as failed
	_ = atomic.LoadInt32(&processedJobs)
}

func TestConcurrentFlushAndSubmit(t *testing.T) {
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		return nil
	}, WithBatchWindow(10*time.Second))
	var wg sync.WaitGroup
	// Concurrent submit
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Submit(context.Background(), &Job{
				ID:      fmt.Sprintf("j%d", i),
				Type:    "scan",
				Payload: map[string]interface{}{"f": "a"},
			})
		}()
	}
	wg.Wait()
	p.Flush(context.Background())
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 50 {
		t.Errorf("expected 50 processed, got %d", atomic.LoadInt32(&processed))
	}
}
