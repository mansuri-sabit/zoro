package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// DBQuery represents a database query operation
type DBQuery struct {
	Table     string
	Operation string // SELECT, INSERT, UPDATE, DELETE
	Context   context.Context
}

// ExecuteWithSpan executes a database query with OpenTelemetry instrumentation
// This wraps MongoDB operations to add DB spans
func ExecuteWithSpan(ctx context.Context, table, operation string, fn func() ([]byte, int64, error)) ([]byte, int64, error) {
	// Get tracer from global provider (works even if not explicitly initialized)
	tracer := otel.Tracer("api-gateway")

	spanName := fmt.Sprintf("db.%s", operation)
	spanCtx, span := tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.DBSystemKey.String("mongodb"),
			semconv.DBOperationKey.String(operation),
			attribute.String("db.collection", table), // MongoDB uses collections, not tables
		),
	)
	defer span.End()

	// Execute the query
	result, count, err := fn()

	// Record result attributes
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(
			attribute.Bool("db.error", true),
			attribute.String("db.error.message", err.Error()),
		)
	} else {
		span.SetAttributes(
			attribute.Bool("db.error", false),
		)
	}

	// Record count if available
	if count > 0 {
		span.SetAttributes(
			attribute.Int64("db.result.count", count),
		)
	}

	// Store span context for potential child spans
	_ = spanCtx

	return result, count, err
}

// ExecuteSelect wraps a SELECT query with DB span
func ExecuteSelect(ctx context.Context, table string, fn func() ([]byte, int64, error)) ([]byte, int64, error) {
	return ExecuteWithSpan(ctx, table, "SELECT", fn)
}

// ExecuteInsert wraps an INSERT query with DB span
func ExecuteInsert(ctx context.Context, table string, fn func() ([]byte, int64, error)) ([]byte, int64, error) {
	return ExecuteWithSpan(ctx, table, "INSERT", fn)
}

// ExecuteUpdate wraps an UPDATE query with DB span
func ExecuteUpdate(ctx context.Context, table string, fn func() ([]byte, int64, error)) ([]byte, int64, error) {
	return ExecuteWithSpan(ctx, table, "UPDATE", fn)
}

// ExecuteDelete wraps a DELETE query with DB span
func ExecuteDelete(ctx context.Context, table string, fn func() ([]byte, int64, error)) ([]byte, int64, error) {
	return ExecuteWithSpan(ctx, table, "DELETE", fn)
}
