// Package batch provides batch processing for LLM requests.
// It groups similar requests together to reduce API calls, lower costs,
// and improve throughput for high-volume workloads.
package batch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Job represents a single item in a batch.
type Job struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"` // code_review, scan, etc.
	Payload   map[string]interface{} `json:"payload"`
	Priority  int                    `json:"priority"` // higher = processed first
	Result    interface{}            `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Status    string                 `json:"status"` // pending, processing, completed, failed
	CreatedAt time.Time              `json:"created_at"`
}

// Batch groups similar jobs for collective processing.
type Batch struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"` // grouping key (hash of type + similar payload)
	Jobs      []*Job    `json:"jobs"`
	CreatedAt time.Time `json:"created_at"`
}

// Processor manages batch creation and processing.
type Processor struct {
	mu           sync.RWMutex
	batches      map[string]*Batch
	processFn    func(ctx context.Context, batch *Batch) error
	maxBatchSize int
	batchWindow  time.Duration
	timer        *time.Timer
}

// NewProcessor creates a new batch processor.
func NewProcessor(processFn func(ctx context.Context, batch *Batch) error, opts ...Option) *Processor {
	p := &Processor{
		batches:     make(map[string]*Batch),
		processFn:   processFn,
		maxBatchSize: 32,
		batchWindow: 100 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Option configures the batch processor.
type Option func(*Processor)

// WithMaxBatchSize sets the maximum jobs per batch.
func WithMaxBatchSize(n int) Option {
	return func(p *Processor) { p.maxBatchSize = n }
}

// WithBatchWindow sets the max wait time before processing a partial batch.
func WithBatchWindow(d time.Duration) Option {
	return func(p *Processor) { p.batchWindow = d }
}

// computeGroupKey generates a grouping key for similar jobs.
// Uses concatenation (not XOR) to avoid key collisions from reordering.
func computeGroupKey(jobType string, payload map[string]interface{}) string {
	var sb strings.Builder
	sb.WriteString(jobType)
	// Sort keys for deterministic ordering
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(fmt.Sprintf("%v", payload[k]))
		sb.WriteString(";")
	}
	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:8])
}

// Submit adds a job to the processing queue.
func (p *Processor) Submit(ctx context.Context, job *Job) error {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.Status = "pending"

	key := computeGroupKey(job.Type, job.Payload)

	p.mu.Lock()
	defer p.mu.Unlock()

	batch, ok := p.batches[key]
	if !ok {
		batch = &Batch{
			ID:        fmt.Sprintf("batch-%d", time.Now().UnixNano()),
			Key:       key,
			CreatedAt: time.Now(),
		}
		p.batches[key] = batch
	}

	batch.Jobs = append(batch.Jobs, job)

	// If batch is full, process immediately
	if len(batch.Jobs) >= p.maxBatchSize {
		go p.processBatch(batch)
		delete(p.batches, key)
		return nil
	}

	// Schedule processing after batch window if not already scheduled
	if p.timer == nil || !p.timer.Stop() {
		p.timer = time.AfterFunc(p.batchWindow, func() {
			p.mu.Lock()
			batches := make([]*Batch, 0, len(p.batches))
			for key, b := range p.batches {
				batches = append(batches, b)
				delete(p.batches, key)
			}
			p.mu.Unlock()

			for _, b := range batches {
				go p.processBatch(b)
			}
		})
	}

	return nil
}

// processBatch sends a batch to the processing function.
func (p *Processor) processBatch(batch *Batch) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := p.processFn(ctx, batch); err != nil {
		slog.Error("batch processing failed", "batch_id", batch.ID, "error", err)
		for _, job := range batch.Jobs {
			job.Status = "failed"
			job.Error = err.Error()
		}
	}
}

// PendingCount returns the number of pending jobs.
func (p *Processor) PendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, b := range p.batches {
		count += len(b.Jobs)
	}
	return count
}

// Flush forces processing of all pending batches.
func (p *Processor) Flush(ctx context.Context) {
	p.mu.Lock()
	batches := make([]*Batch, 0, len(p.batches))
	for key, b := range p.batches {
		batches = append(batches, b)
		delete(p.batches, key)
	}
	p.mu.Unlock()

	for _, b := range batches {
		p.processBatch(b)
	}
}
