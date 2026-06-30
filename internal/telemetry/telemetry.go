package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

var metricsHandler http.Handler

// MetricsHandler returns the HTTP handler for Prometheus /metrics endpoint.
func MetricsHandler() http.Handler {
	return metricsHandler
}

// Setup initializes OpenTelemetry tracing and metrics.
// Returns a cleanup function that should be called on shutdown.
func Setup(ctx context.Context, serviceName, serviceVersion string) (func(), error) {
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
	promExporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// Create meter provider with Prometheus reader
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(promExporter),
	)
	otel.SetMeterProvider(meterProvider)

	// Expose metrics via the standard promhttp handler (reads from default registry)
	metricsHandler = promhttp.Handler()

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

	cleanup := func() {
		if err := tracerProvider.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown tracer provider", "error", err)
		}
		if err := meterProvider.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown meter provider", "error", err)
		}
	}

	return cleanup, nil
}
