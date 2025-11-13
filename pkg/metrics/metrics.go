package metrics

import (
	"fmt"
	"sync"
	"time"
)

// Metrics holds application metrics
type Metrics struct {
	mu sync.RWMutex

	// Request metrics
	TotalRequests     int64
	SuccessfulRequests int64
	FailedRequests    int64

	// Endpoint metrics
	EndpointRequests map[string]int64
	EndpointErrors   map[string]int64
	EndpointLatency  map[string][]time.Duration

	// Service metrics
	ServiceCalls     map[string]int64
	ServiceErrors    map[string]int64
	ServiceLatency   map[string][]time.Duration

	// Circuit breaker metrics
	CircuitBreakerState map[string]string
	CircuitBreakerFailures map[string]int64

	// Start time
	StartTime time.Time
}

var globalMetrics = &Metrics{
	EndpointRequests:      make(map[string]int64),
	EndpointErrors:        make(map[string]int64),
	EndpointLatency:       make(map[string][]time.Duration),
	ServiceCalls:          make(map[string]int64),
	ServiceErrors:         make(map[string]int64),
	ServiceLatency:        make(map[string][]time.Duration),
	CircuitBreakerState:   make(map[string]string),
	CircuitBreakerFailures: make(map[string]int64),
	StartTime:             time.Now(),
}

// RecordRequest records a request
func RecordRequest(endpoint string, success bool, latency time.Duration) {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()

	globalMetrics.TotalRequests++
	if success {
		globalMetrics.SuccessfulRequests++
	} else {
		globalMetrics.FailedRequests++
		globalMetrics.EndpointErrors[endpoint]++
	}

	globalMetrics.EndpointRequests[endpoint]++
	
	// Keep only last 100 latency measurements per endpoint
	if len(globalMetrics.EndpointLatency[endpoint]) >= 100 {
		globalMetrics.EndpointLatency[endpoint] = globalMetrics.EndpointLatency[endpoint][1:]
	}
	globalMetrics.EndpointLatency[endpoint] = append(globalMetrics.EndpointLatency[endpoint], latency)
}

// RecordServiceCall records a service call
func RecordServiceCall(service string, success bool, latency time.Duration) {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()

	globalMetrics.ServiceCalls[service]++
	if !success {
		globalMetrics.ServiceErrors[service]++
	}

	// Keep only last 100 latency measurements per service
	if len(globalMetrics.ServiceLatency[service]) >= 100 {
		globalMetrics.ServiceLatency[service] = globalMetrics.ServiceLatency[service][1:]
	}
	globalMetrics.ServiceLatency[service] = append(globalMetrics.ServiceLatency[service], latency)
}

// UpdateCircuitBreaker updates circuit breaker metrics
func UpdateCircuitBreaker(service, state string, failures int64) {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()

	globalMetrics.CircuitBreakerState[service] = state
	globalMetrics.CircuitBreakerFailures[service] = failures
}

// GetMetrics returns current metrics
func GetMetrics() map[string]interface{} {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	// Calculate average latencies
	endpointAvgLatency := make(map[string]float64)
	for endpoint, latencies := range globalMetrics.EndpointLatency {
		if len(latencies) > 0 {
			var sum time.Duration
			for _, l := range latencies {
				sum += l
			}
			endpointAvgLatency[endpoint] = sum.Seconds() / float64(len(latencies))
		}
	}

	serviceAvgLatency := make(map[string]float64)
	for service, latencies := range globalMetrics.ServiceLatency {
		if len(latencies) > 0 {
			var sum time.Duration
			for _, l := range latencies {
				sum += l
			}
			serviceAvgLatency[service] = sum.Seconds() / float64(len(latencies))
		}
	}

	uptime := time.Since(globalMetrics.StartTime)

	return map[string]interface{}{
		"uptime_seconds": uptime.Seconds(),
		"requests": map[string]interface{}{
			"total":     globalMetrics.TotalRequests,
			"successful": globalMetrics.SuccessfulRequests,
			"failed":    globalMetrics.FailedRequests,
		},
		"endpoints": map[string]interface{}{
			"requests": globalMetrics.EndpointRequests,
			"errors":   globalMetrics.EndpointErrors,
			"latency_avg_seconds": endpointAvgLatency,
		},
		"services": map[string]interface{}{
			"calls":    globalMetrics.ServiceCalls,
			"errors":   globalMetrics.ServiceErrors,
			"latency_avg_seconds": serviceAvgLatency,
		},
		"circuit_breakers": map[string]interface{}{
			"state":    globalMetrics.CircuitBreakerState,
			"failures": globalMetrics.CircuitBreakerFailures,
		},
	}
}

// GetPrometheusMetrics returns metrics in Prometheus format
func GetPrometheusMetrics() string {
	metrics := GetMetrics()
	var output string

	// Uptime
	output += "# HELP api_uptime_seconds API uptime in seconds\n"
	output += "# TYPE api_uptime_seconds gauge\n"
	output += fmt.Sprintf("api_uptime_seconds %.2f\n", metrics["uptime_seconds"].(float64))

	// Requests
	reqs := metrics["requests"].(map[string]interface{})
	output += "# HELP api_requests_total Total number of requests\n"
	output += "# TYPE api_requests_total counter\n"
	output += fmt.Sprintf("api_requests_total{status=\"total\"} %d\n", reqs["total"].(int64))
	output += fmt.Sprintf("api_requests_total{status=\"successful\"} %d\n", reqs["successful"].(int64))
	output += fmt.Sprintf("api_requests_total{status=\"failed\"} %d\n", reqs["failed"].(int64))

	// Endpoint requests
	endpoints := metrics["endpoints"].(map[string]interface{})
	endpointReqs := endpoints["requests"].(map[string]int64)
	output += "# HELP api_endpoint_requests_total Total requests per endpoint\n"
	output += "# TYPE api_endpoint_requests_total counter\n"
	for endpoint, count := range endpointReqs {
		output += fmt.Sprintf("api_endpoint_requests_total{endpoint=\"%s\"} %d\n", endpoint, count)
	}

	// Endpoint errors
	endpointErrs := endpoints["errors"].(map[string]int64)
	output += "# HELP api_endpoint_errors_total Total errors per endpoint\n"
	output += "# TYPE api_endpoint_errors_total counter\n"
	for endpoint, count := range endpointErrs {
		output += fmt.Sprintf("api_endpoint_errors_total{endpoint=\"%s\"} %d\n", endpoint, count)
	}

	// Service calls
	services := metrics["services"].(map[string]interface{})
	serviceCalls := services["calls"].(map[string]int64)
	output += "# HELP api_service_calls_total Total calls per service\n"
	output += "# TYPE api_service_calls_total counter\n"
	for service, count := range serviceCalls {
		output += fmt.Sprintf("api_service_calls_total{service=\"%s\"} %d\n", service, count)
	}

	return output
}

