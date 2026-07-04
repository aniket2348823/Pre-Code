package observability

import (
	"fmt"
	"testing"
)

func TestTracerStartEndSpan(t *testing.T) {
	tr := NewTracer()
	_, span := tr.StartSpan(nil, "test-span")
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	if span.Name != "test-span" {
		t.Errorf("expected name test-span, got %s", span.Name)
	}
	if span.TraceID == "" {
		t.Error("expected trace ID to be set")
	}
	tr.EndSpan(span)
	if tr.SpanCount() != 1 {
		t.Errorf("expected 1 span, got %d", tr.SpanCount())
	}
}

func TestTracerTraceIDFromContext(t *testing.T) {
	tr := NewTracer()
	ctx, _ := tr.StartSpan(nil, "test")
	tid := TraceIDFromContext(ctx)
	if tid == "" {
		t.Error("expected trace ID in context")
	}
}

func TestTracerSetSpanError(t *testing.T) {
	tr := NewTracer()
	_, span := tr.StartSpan(nil, "test")
	tr.SetSpanError(span, fmt.Errorf("boom"))
	tr.EndSpan(span)
	spans := tr.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status != "error" {
		t.Errorf("expected error status, got %s", spans[0].Status)
	}
	if spans[0].Error != "boom" {
		t.Errorf("expected error message boom, got %s", spans[0].Error)
	}
}

func TestTracerGetSpansByTrace(t *testing.T) {
	tr := NewTracer()
	ctx, span1 := tr.StartSpan(nil, "span-1")
	tr.EndSpan(span1)
	_, span2 := tr.StartSpan(ctx, "span-2")
	tr.EndSpan(span2)
	spans := tr.GetSpansByTrace(TraceIDFromContext(ctx))
	if len(spans) != 2 {
		t.Errorf("expected 2 spans, got %d", len(spans))
	}
}

func TestSetSpanAttr(t *testing.T) {
	_, span := NewTracer().StartSpan(nil, "test")
	SetSpanAttr(span, "model", "gpt-4o")
	SetSpanAttr(span, "tokens", "1500")
	if span.Attrs["model"] != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %s", span.Attrs["model"])
	}
}

func TestPerformanceMetricsRecordRequest(t *testing.T) {
	pm := NewPerformanceMetrics()
	pm.RecordRequest(50, false)
	pm.RecordRequest(200, false)
	pm.RecordRequest(600, true)
	summary := pm.Summary()
	if summary["total_requests"] != int64(3) {
		t.Errorf("expected 3 requests, got %v", summary["total_requests"])
	}
	if summary["total_errors"] != int64(1) {
		t.Errorf("expected 1 error, got %v", summary["total_errors"])
	}
}

func TestPerformanceMetricsErrorRate(t *testing.T) {
	pm := NewPerformanceMetrics()
	for i := 0; i < 10; i++ {
		pm.RecordRequest(50, i < 2)
	}
	summary := pm.Summary()
	if summary["error_rate_pct"] != 20.0 {
		t.Errorf("expected 20%% error rate, got %v", summary["error_rate_pct"])
	}
}

func TestPerformanceMetricsReset(t *testing.T) {
	pm := NewPerformanceMetrics()
	pm.RecordRequest(50, false)
	pm.Reset()
	summary := pm.Summary()
	if summary["total_requests"] != int64(0) {
		t.Errorf("expected 0 requests after reset, got %v", summary["total_requests"])
	}
}

func TestPerformanceMetricsLatencyBuckets(t *testing.T) {
	pm := NewPerformanceMetrics()
	pm.RecordRequest(50, false)   // 0-100ms
	pm.RecordRequest(250, false)  // 100-500ms
	pm.RecordRequest(750, false)  // 500ms-1s
	pm.RecordRequest(3000, false) // 1s-5s
	pm.RecordRequest(10000, false) // 5s+
	summary := pm.Summary()
	buckets := summary["latency_buckets"].(map[string]int64)
	if buckets["0-100ms"] != 1 {
		t.Errorf("expected 1 in 0-100ms, got %d", buckets["0-100ms"])
	}
	if buckets["5s+"] != 1 {
		t.Errorf("expected 1 in 5s+, got %d", buckets["5s+"])
	}
}

func TestWithTraceID(t *testing.T) {
	ctx := WithTraceID(nil, "test-trace-123")
	tid := TraceIDFromContext(ctx)
	if tid != "test-trace-123" {
		t.Errorf("expected test-trace-123, got %s", tid)
	}
}

func TestPerformanceMetricsAvgDuration(t *testing.T) {
	pm := NewPerformanceMetrics()
	pm.RecordRequest(100, false)
	pm.RecordRequest(200, false)
	pm.RecordRequest(300, false)
	summary := pm.Summary()
	avg := summary["avg_duration_ms"].(float64)
	if avg != 200.0 {
		t.Errorf("expected avg 200ms, got %f", avg)
	}
}
