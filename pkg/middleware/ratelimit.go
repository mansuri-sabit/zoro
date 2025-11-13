package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	client      *redis.Client
	maxRequests int
	windowSec   int
}

func NewRateLimiter(client *redis.Client, maxRequestsPerMinute int) *RateLimiter {
	return &RateLimiter{
		client:      client,
		maxRequests: maxRequestsPerMinute,
		windowSec:   60,
	}
}

func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			userID = c.ClientIP()
		}

		key := fmt.Sprintf("ratelimit:%v", userID)
		ctx := context.Background()

		count, err := rl.client.Incr(ctx, key).Result()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rate limit check failed"})
			c.Abort()
			return
		}

		if count == 1 {
			rl.client.Expire(ctx, key, time.Duration(rl.windowSec)*time.Second)
		}

		if count > int64(rl.maxRequests) {
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", rl.maxRequests))
			c.Header("X-RateLimit-Remaining", "0")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
				"retry_after": rl.windowSec,
			})
			c.Abort()
			return
		}

		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", rl.maxRequests))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", rl.maxRequests-int(count)))
		c.Next()
	}
}

type TokenBucket struct {
	client       *redis.Client
	capacity     int64
	refillRate   int64
	refillPeriod time.Duration
}

func NewTokenBucket(client *redis.Client, capacity int64, refillRate int64, refillPeriod time.Duration) *TokenBucket {
	return &TokenBucket{
		client:       client,
		capacity:     capacity,
		refillRate:   refillRate,
		refillPeriod: refillPeriod,
	}
}

func (tb *TokenBucket) TakeToken(ctx context.Context, key string) (bool, error) {
	bucketKey := fmt.Sprintf("token_bucket:%s", key)
	lastRefillKey := fmt.Sprintf("token_bucket_refill:%s", key)

	now := time.Now().Unix()

	lastRefillStr, err := tb.client.Get(ctx, lastRefillKey).Result()
	var lastRefill int64
	if err == redis.Nil {
		lastRefill = now
		tb.client.Set(ctx, lastRefillKey, now, 0)
		tb.client.Set(ctx, bucketKey, tb.capacity, 0)
	} else if err != nil {
		return false, err
	} else {
		fmt.Sscanf(lastRefillStr, "%d", &lastRefill)
	}

	elapsed := now - lastRefill
	refillAmount := (elapsed / int64(tb.refillPeriod.Seconds())) * tb.refillRate

	if refillAmount > 0 {
		currentTokens, _ := tb.client.Get(ctx, bucketKey).Int64()
		newTokens := currentTokens + refillAmount
		if newTokens > tb.capacity {
			newTokens = tb.capacity
		}
		tb.client.Set(ctx, bucketKey, newTokens, 0)
		tb.client.Set(ctx, lastRefillKey, now, 0)
	}

	tokens, err := tb.client.Decr(ctx, bucketKey).Result()
	if err != nil {
		return false, err
	}

	if tokens >= 0 {
		return true, nil
	}

	tb.client.Incr(ctx, bucketKey)
	return false, nil
}
