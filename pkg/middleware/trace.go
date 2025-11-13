package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

const traceIDHeader = "X-Trace-ID"
const requestIDHeader = "X-Request-ID"

// TraceMiddleware adds trace ID and request ID to context
func TraceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get or generate trace ID
		traceID := c.GetHeader(traceIDHeader)
		if traceID == "" {
			traceID = generateID()
		}

		// Generate request ID
		requestID := generateID()

		// Set in context
		c.Set("trace_id", traceID)
		c.Set("request_id", requestID)

		// Add to response headers
		c.Header(traceIDHeader, traceID)
		c.Header(requestIDHeader, requestID)

		c.Next()
	}
}

func generateID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

