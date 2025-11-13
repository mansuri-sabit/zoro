package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// Config holds circuit breaker configuration
type Config struct {
	FailureThreshold int           // Number of failures before opening
	SuccessThreshold int           // Number of successes to close from half-open
	Timeout          time.Duration // Time to wait before attempting half-open
	ResetTimeout     time.Duration // Time before resetting failure count
}

// DefaultConfig returns a default circuit breaker configuration
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		ResetTimeout:     60 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config        Config
	state         State
	failures      int
	successes     int
	lastFailTime  time.Time
	lastResetTime time.Time
	mu            sync.RWMutex
}

// New creates a new circuit breaker
func New(config Config) *CircuitBreaker {
	return &CircuitBreaker{
		config:        config,
		state:         StateClosed,
		lastResetTime: time.Now(),
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.Lock()
	
	// Check if we should transition states
	cb.updateState()
	
	state := cb.state
	cb.mu.Unlock()

	switch state {
	case StateOpen:
		return errors.New("circuit breaker is open")
	case StateHalfOpen:
		// Allow one request through
		err := fn()
		cb.recordResult(err)
		return err
	case StateClosed:
		err := fn()
		cb.recordResult(err)
		return err
	default:
		return errors.New("unknown circuit breaker state")
	}
}

// updateState updates the circuit breaker state based on current conditions
func (cb *CircuitBreaker) updateState() {
	now := time.Now()

	// Reset failure count if reset timeout has passed
	if now.Sub(cb.lastResetTime) > cb.config.ResetTimeout {
		cb.failures = 0
		cb.lastResetTime = now
	}

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = StateOpen
			cb.lastFailTime = now
		}
	case StateOpen:
		if now.Sub(cb.lastFailTime) >= cb.config.Timeout {
			cb.state = StateHalfOpen
			cb.successes = 0
		}
	case StateHalfOpen:
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = StateClosed
			cb.failures = 0
		} else if cb.failures > 0 {
			cb.state = StateOpen
			cb.lastFailTime = now
		}
	}
}

// recordResult records the result of an operation
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFailTime = time.Now()
		if cb.state == StateHalfOpen {
			cb.successes = 0
		}
	} else {
		cb.failures = 0
		if cb.state == StateHalfOpen {
			cb.successes++
		}
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stateStr := "closed"
	switch cb.state {
	case StateOpen:
		stateStr = "open"
	case StateHalfOpen:
		stateStr = "half-open"
	}

	return map[string]interface{}{
		"state":      stateStr,
		"failures":   cb.failures,
		"successes":  cb.successes,
		"last_fail":  cb.lastFailTime,
		"last_reset": cb.lastResetTime,
	}
}

