// Package cachewarm provides a background cache warming service that
// pre-computes and caches expensive operations.
package cachewarm

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// WarmerFunc is a function that computes a value to cache.
type WarmerFunc func(ctx context.Context) (interface{}, error)

// Entry holds a cached value with metadata.
type Entry struct {
	Value     interface{}
	Err       error
	UpdatedAt time.Time
}

// Warmer manages background cache warming jobs.
type Warmer struct {
	mu      sync.RWMutex
	jobs    map[string]*job
	stopped bool
}

type job struct {
	key      string
	interval time.Duration
	fn       WarmerFunc
	entry    *Entry
	stopCh   chan struct{}
}

// NewWarmer creates a new cache warming service.
func NewWarmer() *Warmer {
	return &Warmer{
		jobs: make(map[string]*job),
	}
}

// Register adds a cache warming job. The fn is called at the given interval
// and the result is stored under the given key.
func (w *Warmer) Register(key string, interval time.Duration, fn WarmerFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()

	j := &job{
		key:      key,
		interval: interval,
		fn:       fn,
		entry:    &Entry{},
		stopCh:   make(chan struct{}),
	}
	w.jobs[key] = j
}

// Start begins all registered warming jobs.
func (w *Warmer) Start(ctx context.Context) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for _, j := range w.jobs {
		go w.runJob(ctx, j)
	}
	slog.Info("cache warmer started", "jobs", len(w.jobs))
}

func (w *Warmer) runJob(ctx context.Context, j *job) {
	// Run immediately on start
	w.executeJob(ctx, j)

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-j.stopCh:
			return
		case <-ticker.C:
			w.executeJob(ctx, j)
		}
	}
}

func (w *Warmer) executeJob(ctx context.Context, j *job) {
	val, err := j.fn(ctx)
	w.mu.Lock()
	j.entry.Value = val
	j.entry.Err = err
	j.entry.UpdatedAt = time.Now()
	w.mu.Unlock()

	if err != nil {
		slog.Warn("cache warmer job failed", "key", j.key, "error", err)
	} else {
		slog.Debug("cache warmer job completed", "key", j.key)
	}
}

// Get retrieves a cached value by key.
func (w *Warmer) Get(key string) (*Entry, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	j, ok := w.jobs[key]
	if !ok {
		return nil, false
	}
	return j.entry, true
}

// Stop stops all warming jobs.
func (w *Warmer) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return
	}
	w.stopped = true
	for _, j := range w.jobs {
		close(j.stopCh)
	}
	slog.Info("cache warmer stopped")
}

// Keys returns all registered cache keys.
func (w *Warmer) Keys() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	keys := make([]string, 0, len(w.jobs))
	for k := range w.jobs {
		keys = append(keys, k)
	}
	return keys
}
