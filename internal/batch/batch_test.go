package batch

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewProcessor(t *testing.T) {
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		return nil
	})
	if p == nil {
		t.Fatal("expected non-nil processor")
	}
}

func TestComputeGroupKey(t *testing.T) {
	key1 := computeGroupKey("scan", map[string]interface{}{"file": "a.go"})
	key2 := computeGroupKey("scan", map[string]interface{}{"file": "a.go"})
	key3 := computeGroupKey("scan", map[string]interface{}{"file": "b.go"})
	if key1 != key2 {
		t.Error("expected same key for same input")
	}
	if key1 == key3 {
		t.Error("expected different keys for different input")
	}
}

func TestSubmitAndProcess(t *testing.T) {
	processed := int32(0)
	var mu sync.Mutex
	var batches []*Batch

	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		mu.Lock()
		batches = append(batches, batch)
		mu.Unlock()
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		for _, j := range batch.Jobs {
			j.Status = "completed"
		}
		return nil
	}, WithBatchWindow(5*time.Second))

	p.Submit(context.Background(), &Job{ID: "j1", Type: "scan", Payload: map[string]interface{}{"file": "a.go"}})
	p.Submit(context.Background(), &Job{ID: "j2", Type: "scan", Payload: map[string]interface{}{"file": "a.go"}})

	p.Flush(context.Background())
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 2 {
		t.Errorf("expected 2 processed jobs, got %d", atomic.LoadInt32(&processed))
	}
}

func TestMaxBatchSize(t *testing.T) {
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		return nil
	}, WithMaxBatchSize(3), WithBatchWindow(5*time.Second))

	// Submit 3 jobs with same key — should trigger immediate processing
	for i := 0; i < 3; i++ {
		p.Submit(context.Background(), &Job{
			ID:      "j" + string(rune('0'+i)),
			Type:    "scan",
			Payload: map[string]interface{}{"file": "a.go"},
		})
	}

	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 3 {
		t.Errorf("expected 3 processed jobs (immediate flush), got %d", atomic.LoadInt32(&processed))
	}
}

func TestPendingCount(t *testing.T) {
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		return nil
	}, WithBatchWindow(10*time.Second))

	p.Submit(context.Background(), &Job{ID: "j1", Type: "scan", Payload: map[string]interface{}{"f": "a"}})
	count := p.PendingCount()
	if count != 1 {
		t.Errorf("expected 1 pending, got %d", count)
	}
}

func TestFlush(t *testing.T) {
	processed := int32(0)
	p := NewProcessor(func(ctx context.Context, batch *Batch) error {
		atomic.AddInt32(&processed, int32(len(batch.Jobs)))
		return nil
	}, WithBatchWindow(10*time.Second))

	p.Submit(context.Background(), &Job{ID: "j1", Type: "scan", Payload: map[string]interface{}{"f": "x"}})
	p.Submit(context.Background(), &Job{ID: "j2", Type: "scan", Payload: map[string]interface{}{"f": "y"}})

	p.Flush(context.Background())
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 2 {
		t.Errorf("expected 2 after flush, got %d", atomic.LoadInt32(&processed))
	}
}
