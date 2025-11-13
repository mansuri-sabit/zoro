package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var Tracer trace.Tracer

// InitTracing initializes OpenTelemetry tracing
func InitTracing(serviceName, serviceVersion, otelEndpoint string) (func(), error) {
	ctx := context.Background()

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter
	var exporter sdktrace.SpanExporter
	if otelEndpoint != "" {
		exporter, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(otelEndpoint),
			otlptracehttp.WithInsecure(), // Use WithTLSClientConfig for production
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create exporter: %w", err)
		}
	} else {
		// No-op exporter if endpoint not configured
		exporter = &noopExporter{}
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	Tracer = tp.Tracer(serviceName)

	// Return shutdown function
	return func() {
		if err := tp.Shutdown(ctx); err != nil {
			// Log error but don't fail
		}
	}, nil
}

// noopExporter is a no-op span exporter
type noopExporter struct{}

func (e *noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *noopExporter) Shutdown(ctx context.Context) error {
	return nil
}
