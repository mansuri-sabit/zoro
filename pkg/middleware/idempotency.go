package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const idempotencyKeyHeader = "Idempotency-Key"
const idempotencyTTL = 24 * time.Hour

// IdempotencyMiddleware ensures idempotent requests using Idempotency-Key header
func IdempotencyMiddleware(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only apply to POST, PUT, PATCH
		if c.Request.Method != http.MethodPost &&
			c.Request.Method != http.MethodPut &&
			c.Request.Method != http.MethodPatch {
			c.Next()
			return
		}

		key := c.GetHeader(idempotencyKeyHeader)
		if key == "" {
			c.Next()
			return
		}

		// Hash the key for storage
		hash := hashIdempotencyKey(key)
		cacheKey := "idempotency:" + hash

		// Check if we've seen this key
		ctx := c.Request.Context()
		val, err := redisClient.Get(ctx, cacheKey).Result()
		if err == nil && val != "" {
			// Return cached response
			c.Header("X-Idempotency-Key-Used", "true")
			c.Data(http.StatusOK, "application/json", []byte(val))
			c.Abort()
			return
		}

		// Store the key (will be populated after response)
		c.Set("idempotency_key", hash)
		c.Set("idempotency_cache_key", cacheKey)
		c.Next()
	}
}

// StoreIdempotencyResponse stores the response for idempotency
func StoreIdempotencyResponse(c *gin.Context, redisClient *redis.Client, status int, body []byte) {
	cacheKey, exists := c.Get("idempotency_cache_key")
	if !exists {
		return
	}

	// Store response body
	ctx := c.Request.Context()
	redisClient.Set(ctx, cacheKey.(string), string(body), idempotencyTTL)
}

func hashIdempotencyKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}
