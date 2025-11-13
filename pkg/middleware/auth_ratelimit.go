package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// AuthRateLimiter provides stricter rate limiting for authentication endpoints
type AuthRateLimiter struct {
	client      *redis.Client
	maxAttempts int
	windowSec   int
	blockSec    int
}

// NewAuthRateLimiter creates a new auth rate limiter
// maxAttempts: number of attempts allowed in window
// windowSec: time window in seconds
// blockSec: time to block after exceeding limit
func NewAuthRateLimiter(client *redis.Client, maxAttempts, windowSec, blockSec int) *AuthRateLimiter {
	return &AuthRateLimiter{
		client:      client,
		maxAttempts: maxAttempts,
		windowSec:   windowSec,
		blockSec:    blockSec,
	}
}

// Middleware returns a gin middleware for auth rate limiting
func (arl *AuthRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		key := fmt.Sprintf("auth_ratelimit:%s", ip)
		blockKey := fmt.Sprintf("auth_blocked:%s", ip)
		ctx := context.Background()

		// Check if IP is blocked
		blocked, err := arl.client.Exists(ctx, blockKey).Result()
		if err == nil && blocked > 0 {
			ttl, _ := arl.client.TTL(ctx, blockKey).Result()
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", arl.maxAttempts))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("Retry-After", fmt.Sprintf("%d", int(ttl.Seconds())))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":      "too many authentication attempts",
				"retry_after": int(ttl.Seconds()),
			})
			c.Abort()
			return
		}

		// Increment attempt count
		count, err := arl.client.Incr(ctx, key).Result()
		if err != nil {
			// If Redis fails, allow the request (fail open)
			c.Next()
			return
		}

		// Set expiration on first request
		if count == 1 {
			arl.client.Expire(ctx, key, time.Duration(arl.windowSec)*time.Second)
		}

		// Check if limit exceeded
		if count > int64(arl.maxAttempts) {
			// Block the IP
			arl.client.Set(ctx, blockKey, "1", time.Duration(arl.blockSec)*time.Second)

			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", arl.maxAttempts))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("Retry-After", fmt.Sprintf("%d", arl.blockSec))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":      "too many authentication attempts",
				"retry_after": arl.blockSec,
			})
			c.Abort()
			return
		}

		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", arl.maxAttempts))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", arl.maxAttempts-int(count)))
		c.Next()
	}
}

