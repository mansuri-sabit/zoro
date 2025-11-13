package otel

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// GinMiddleware creates OpenTelemetry middleware for Gin
func GinMiddleware() gin.HandlerFunc {
	propagator := otel.GetTextMapPropagator()
	tracer := otel.Tracer("api-gateway")

	return func(c *gin.Context) {
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := tracer.Start(ctx, c.FullPath(),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPMethodKey.String(c.Request.Method),
				semconv.HTTPURLKey.String(c.Request.URL.String()),
				semconv.HTTPRouteKey.String(c.FullPath()),
			),
		)
		defer span.End()

		// Store span in context
		c.Request = c.Request.WithContext(ctx)

		// Process request
		c.Next()

		// Record status
		statusCode := c.Writer.Status()
		span.SetAttributes(
			semconv.HTTPStatusCodeKey.Int(statusCode),
		)

		// Set status based on code
		if statusCode >= 400 {
			span.SetStatus(codes.Error, http.StatusText(statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// Record errors
		if len(c.Errors) > 0 {
			span.RecordError(c.Errors.Last())
		}
	}
}
