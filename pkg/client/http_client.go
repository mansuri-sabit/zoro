package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/troikatech/calling-agent/pkg/circuitbreaker"
	"github.com/troikatech/calling-agent/pkg/metrics"
	"github.com/troikatech/calling-agent/pkg/retry"
)

// HTTPClient wraps http.Client with retry and circuit breaker
type HTTPClient struct {
	client         *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	serviceName    string
}

// NewHTTPClient creates a new HTTP client with retry and circuit breaker
func NewHTTPClient(serviceName string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
		circuitBreaker: circuitbreaker.New(circuitbreaker.DefaultConfig()),
		serviceName:    serviceName,
	}
}

// Post performs a POST request with retry and circuit breaker
func (c *HTTPClient) Post(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	start := time.Now()
	var resp *http.Response
	var err error

	// Execute with circuit breaker
	err = c.circuitBreaker.Execute(ctx, func() error {
		// Execute with retry
		err := retry.Do(ctx, retry.DefaultConfig(), func() error {
			jsonData, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				return marshalErr
			}

			req, reqErr := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
			if reqErr != nil {
				return reqErr
			}
			req.Header.Set("Content-Type", "application/json")

			resp, reqErr = c.client.Do(req)
			if reqErr != nil {
				return reqErr
			}

			if resp.StatusCode >= 500 {
				return fmt.Errorf("server error: %d", resp.StatusCode)
			}

			return nil
		})
		return err
	})

	latency := time.Since(start)
	success := err == nil && resp != nil && resp.StatusCode < 400

	// Record metrics
	metrics.RecordServiceCall(c.serviceName, success, latency)

	// Update circuit breaker state
	state := c.circuitBreaker.GetState()
	stateStr := "closed"
	switch state {
	case circuitbreaker.StateOpen:
		stateStr = "open"
	case circuitbreaker.StateHalfOpen:
		stateStr = "half-open"
	}
	stats := c.circuitBreaker.GetStats()
	failures := int64(0)
	if f, ok := stats["failures"].(int); ok {
		failures = int64(f)
	}
	metrics.UpdateCircuitBreaker(c.serviceName, stateStr, failures)

	return resp, err
}

