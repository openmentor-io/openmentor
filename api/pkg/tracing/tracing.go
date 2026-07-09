package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
)

var tracer trace.Tracer

// InitTracer initializes the OpenTelemetry tracer provider
func InitTracer(serviceName, serviceNamespace, serviceVersion, serviceInstanceID, environment, alloyEndpoint string) (func(context.Context) error, error) {
	if alloyEndpoint == "" {
		logger.Info("Tracing disabled: ALLOY_ENDPOINT not set")
		return func(context.Context) error { return nil }, nil
	}

	logger.Info("Initializing OpenTelemetry tracer",
		zap.String("service", serviceName),
		zap.String("namespace", serviceNamespace),
		zap.String("version", serviceVersion),
		zap.String("environment", environment),
		zap.String("endpoint", alloyEndpoint))

	// Create OTLP HTTP exporter (recommended by Grafana)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(alloyEndpoint),
		otlptracehttp.WithInsecure(), // Alloy is on internal network (no TLS)
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceNamespace(serviceNamespace),
			semconv.ServiceVersion(serviceVersion),
			semconv.ServiceInstanceID(serviceInstanceID),
			attribute.String("deployment.environment.name", environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create batch span processor with proper timeouts
	// This ensures export failures don't block the application
	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithBatchTimeout(2*time.Second),  // Export batch every 2 seconds
		sdktrace.WithExportTimeout(5*time.Second), // Timeout individual exports after 5s
		sdktrace.WithMaxQueueSize(2048),           // Max queue size before dropping
		sdktrace.WithMaxExportBatchSize(512),      // Max spans per export batch
	)

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Sample all traces in production
	)

	// Set global tracer provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Get tracer instance
	tracer = tp.Tracer(serviceName)

	logger.Info("OpenTelemetry tracer initialized successfully")

	// Return shutdown function
	return tp.Shutdown, nil
}

// Tracer returns the global tracer instance
func Tracer() trace.Tracer {
	return tracer
}

// StartSpan starts a new span with the given name
func StartSpan(ctx context.Context, spanName string) (context.Context, trace.Span) {
	if tracer == nil {
		// Return no-op span if tracer not initialized
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, spanName)
}
