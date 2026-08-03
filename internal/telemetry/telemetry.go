package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

var (
	metricsHandler   http.Handler
	metricsHandlerMu sync.RWMutex
)

// MetricsHandler returns the HTTP handler for Prometheus /metrics endpoint.
// After Setup() runs, returns the OTLP-aware handler with exported metrics.
// Before Setup() (e.g., in tests), returns a basic promhttp handler as fallback.
func MetricsHandler() http.Handler {
	metricsHandlerMu.RLock()
	h := metricsHandler
	metricsHandlerMu.RUnlock()
	if h != nil {
		return h
	}
	if setupDone.Load() {
		metricsHandlerMu.RLock()
		defer metricsHandlerMu.RUnlock()
		return metricsHandler
	}
	return promhttp.Handler()
}

// Pre-registered Prometheus metrics for Grafana dashboard panels.
// Uses promauto for safe auto-registration — no panic on double init.
var (
	// NOTE: LLMProviderHealth is registered in internal/llm/health.go
	// to avoid circular imports. It is updated directly by RecordSuccess/RecordFailure.

	// TaskProcessingSeconds tracks task execution latency per provider and model.
	TaskProcessingSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vigilagent_task_processing_seconds",
			Help:    "Task processing duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provider", "model", "status"},
	)
	// TokensConsumedTotal tracks cumulative token usage.
	TokensConsumedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vigilagent_tokens_consumed_total",
			Help: "Total tokens consumed by LLM requests",
		},
		[]string{"provider", "token_type"},
	)
	// ActiveSessions tracks current active sessions per project.
	ActiveSessions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vigilagent_active_sessions",
			Help: "Number of active user sessions",
		},
		[]string{"project_id"},
	)
	// VerificationConfidenceTracks tracks Shift-Zero pipeline confidence scores.
	VerificationConfidenceTracks = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vigilagent_verification_confidence",
			Help:    "Verification confidence score distribution (0.0 to 1.0)",
			Buckets: []float64{0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.95, 0.98, 1.0},
		},
		[]string{"language", "grade"},
	)
	// TaskDuration tracks task execution duration in seconds.
	TaskDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "vigilagent",
			Subsystem: "task",
			Name:      "duration_seconds",
			Help:      "Task execution duration in seconds.",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		},
	)
	// LLMTokens tracks total LLM tokens used by model.
	LLMTokens = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vigilagent",
			Subsystem: "llm",
			Name:      "tokens_total",
			Help:      "Total LLM tokens consumed.",
		},
		[]string{"model", "type"}, // type = input|output
	)
	// NATSQueueDepth tracks the current depth of the NATS JetStream queue.
	NATSQueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "vigilagent",
			Subsystem: "queue",
			Name:      "nats_depth",
			Help:      "Current depth of the NATS JetStream queue.",
		},
	)
	// DroppedSpans tracks the number of OTLP spans dropped due to exporter failures.
	DroppedSpans = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "vigilagent",
			Subsystem: "telemetry",
			Name:      "dropped_spans_total",
			Help:      "Total number of OTLP spans dropped due to exporter failures.",
		},
	)
	// HITLCheckpointsTotal tracks HITL checkpoint submissions and outcomes.
	HITLCheckpointsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vigilagent",
			Subsystem: "hitl",
			Name:      "checkpoints_total",
			Help:      "Total HITL checkpoints by status (pending, approved, rejected, timed_out).",
		},
		[]string{"status"},
	)
)

// setupDone tracks whether Setup() has been called.
// After Setup(), MetricsHandler() returns the OTLP-aware handler.
// Before Setup() (e.g., in tests), MetricsHandler() falls back to basic promhttp.
var setupDone atomic.Bool

// Setup initializes OpenTelemetry tracing and metrics exporting.
func Setup(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	slog.Info("initializing telemetry", "service", serviceName, "version", serviceVersion)

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Setup Prometheus metrics exporter (writes to the default Prometheus registry)
	promExporter, err := otelprom.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// Create meter provider with Prometheus reader
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(promExporter),
	)
	otel.SetMeterProvider(meterProvider)

	// Assign the OTLP-aware promhttp handler (includes OTLP-exported metrics).
	// MetricsHandler() returns this after Setup(); before Setup(), it falls back.
	metricsHandlerMu.Lock()
	metricsHandler = promhttp.Handler()
	metricsHandlerMu.Unlock()
	setupDone.Store(true)

	// Setup trace provider
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tracerProvider)

	slog.Info("opentelemetry initialized",
		"service", serviceName,
		"version", serviceVersion,
	)

	cleanup := func(c context.Context) error {
		if err := tracerProvider.Shutdown(c); err != nil {
			slog.Error("failed to shutdown tracer provider", "error", err)
		}
		if err := meterProvider.Shutdown(c); err != nil {
			slog.Error("failed to shutdown meter provider", "error", err)
		}
		return nil
	}

	return cleanup, nil
}
