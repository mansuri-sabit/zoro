package retry

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Config holds retry configuration
type Config struct {
	MaxAttempts int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Jitter       bool
}

// DefaultConfig returns a default retry configuration
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
	}
}

// Do executes a function with retry logic using exponential backoff
func Do(ctx context.Context, config Config, fn func() error) error {
	var lastErr error
	
	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Execute the function
		err := fn()
		if err == nil {
			return nil
		}
		
		lastErr = err

		// Don't retry on last attempt
		if attempt == config.MaxAttempts-1 {
			break
		}

		// Calculate delay with exponential backoff
		delay := time.Duration(float64(config.InitialDelay) * math.Pow(config.Multiplier, float64(attempt)))
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}

		// Add jitter if enabled
		if config.Jitter {
			delay = addJitter(delay)
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return fmt.Errorf("retry failed after %d attempts: %w", config.MaxAttempts, lastErr)
}

// addJitter adds random jitter to the delay (up to 20%)
func addJitter(delay time.Duration) time.Duration {
	jitter := time.Duration(float64(delay) * 0.2 * (0.5 + 0.5*float64(time.Now().UnixNano()%100)/100))
	return delay + jitter
}

