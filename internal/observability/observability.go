// Package observability provides structured logging, request tracing, and
// performance metrics for all VigilAgent components.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type contextKey string

const traceIDKey contextKey = "trace_id"

// TraceID is the unique identifier for a request trace.
type TraceID string

// Span represents a single unit of work within a trace.
type Span struct {
	TraceID   TraceID       `json:"trace_id"`
	SpanID    string        `json:"span_id"`
	ParentID  string        `json:"parent_id,omitempty"`
	Name      string        `json:"name"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time,omitempty"`
	Duration  time.Duration `json:"duration"`
	Status    string        `json:"status"` // ok, error
	Error     string        `json:"error,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// Tracer collects spans for distributed tracing.
type Tracer struct {
	mu    sync.RWMutex
	spans []Span
}

// NewTracer creates a new tracing engine.
func NewTracer() *Tracer {
	return &Tracer{}
}

// StartSpan begins a new span and returns a context with the trace ID.
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	traceID, _ := ctx.Value(traceIDKey).(TraceID)
	if traceID == "" {
		traceID = TraceID(fmt.Sprintf("trace-%d", time.Now().UnixNano()))
	}

	span := &Span{
		TraceID:   traceID,
		SpanID:    fmt.Sprintf("span-%d", time.Now().UnixNano()),
		Name:      name,
		StartTime: time.Now(),
		Status:    "ok",
		Attrs:     make(map[string]string),
	}

	return context.WithValue(ctx, traceIDKey, traceID), span
}

// EndSpan finalizes a span.
func (t *Tracer) EndSpan(span *Span) {
	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.spans = append(t.spans, *span)

	slog.Debug("span completed",
		"trace_id", span.TraceID,
		"span_id", span.SpanID,
		"name", span.Name,
		"duration_ms", span.Duration.Milliseconds(),
		"status", span.Status,
	)
}

// SetSpanError marks a span as errored.
func (t *Tracer) SetSpanError(span *Span, err error) {
	span.Status = "error"
	span.Error = err.Error()
}

// SetSpanAttr adds a key-value attribute to a span.
func SetSpanAttr(span *Span, key, value string) {
	if span.Attrs == nil {
		span.Attrs = make(map[string]string)
	}
	span.Attrs[key] = value
}

// GetSpans returns all recorded spans.
func (t *Tracer) GetSpans() []Span {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Span, len(t.spans))
	copy(out, t.spans)
	return out
}

// GetSpansByTrace returns spans for a specific trace.
func (t *Tracer) GetSpansByTrace(traceID TraceID) []Span {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []Span
	for _, s := range t.spans {
		if s.TraceID == traceID {
			out = append(out, s)
		}
	}
	return out
}

// SpanCount returns total span count.
func (t *Tracer) SpanCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.spans)
}

// WithTraceID adds a trace ID to a context.
func WithTraceID(ctx context.Context, id TraceID) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceIDFromContext extracts the trace ID from a context.
func TraceIDFromContext(ctx context.Context) TraceID {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(traceIDKey).(TraceID)
	return id
}

// PerformanceMetrics tracks aggregated performance data.
type PerformanceMetrics struct {
	mu              sync.RWMutex
	requestCount    int64
	totalDurationMs int64
	errorCount      int64
	latencyBuckets  map[string]int64 // "0-100ms", "100-500ms", etc.
}

// NewPerformanceMetrics creates a new metrics collector.
func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{
		latencyBuckets: map[string]int64{
			"0-100ms":    0,
			"100-500ms":  0,
			"500ms-1s":   0,
			"1s-5s":      0,
			"5s+":        0,
		},
	}
}

// RecordRequest records a completed request.
func (pm *PerformanceMetrics) RecordRequest(durationMs int64, isError bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.requestCount++
	pm.totalDurationMs += durationMs
	if isError {
		pm.errorCount++
	}

	switch {
	case durationMs < 100:
		pm.latencyBuckets["0-100ms"]++
	case durationMs < 500:
		pm.latencyBuckets["100-500ms"]++
	case durationMs < 1000:
		pm.latencyBuckets["500ms-1s"]++
	case durationMs < 5000:
		pm.latencyBuckets["1s-5s"]++
	default:
		pm.latencyBuckets["5s+"]++
	}
}

// Summary returns aggregated metrics.
func (pm *PerformanceMetrics) Summary() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var avgDuration float64
	if pm.requestCount > 0 {
		avgDuration = float64(pm.totalDurationMs) / float64(pm.requestCount)
	}
	var errorRate float64
	if pm.requestCount > 0 {
		errorRate = float64(pm.errorCount) / float64(pm.requestCount) * 100
	}

	buckets := make(map[string]int64, len(pm.latencyBuckets))
	for k, v := range pm.latencyBuckets {
		buckets[k] = v
	}

	return map[string]interface{}{
		"total_requests": pm.requestCount,
		"total_errors":   pm.errorCount,
		"avg_duration_ms": avgDuration,
		"error_rate_pct": errorRate,
		"latency_buckets": buckets,
	}
}

// Reset clears all metrics.
func (pm *PerformanceMetrics) Reset() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.requestCount = 0
	pm.totalDurationMs = 0
	pm.errorCount = 0
	for k := range pm.latencyBuckets {
		pm.latencyBuckets[k] = 0
	}
}
