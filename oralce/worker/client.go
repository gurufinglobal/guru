// Package worker handles external data fetching and processing for Oracle operations
// Implements HTTP client with circuit breaker, rate limiting, and retry logic
package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
)

// circuitBreaker implements circuit breaker pattern for HTTP requests
// Prevents cascading failures by temporarily blocking requests after repeated failures
type circuitBreaker struct {
	openUntil    int64 // Unix nano timestamp until which breaker is open
	failures     int64 // Rolling failure count within current window
	lastWindowTs int64 // Unix timestamp for current failure window
}

// newCircuitBreaker creates a new circuit breaker instance
// Initializes with closed state and zero failures
func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{
		openUntil:    0,
		failures:     0,
		lastWindowTs: 0,
	}
}

// allow checks if requests are currently allowed through the circuit breaker
// Returns false if breaker is open due to recent failures
func (c *circuitBreaker) allow() bool {
	return atomic.LoadInt64(&c.openUntil) <= time.Now().UnixNano()
}

// onFailure records a failure and potentially opens the circuit breaker
// Implements sliding window failure counting and automatic breaker opening
func (c *circuitBreaker) onFailure() {
	// Reset failure count on new time window
	window := time.Now().Unix() / int64(max(1, config.RetryCBWindowSec()))
	if atomic.LoadInt64(&c.lastWindowTs) != window {
		atomic.StoreInt64(&c.lastWindowTs, window)
		atomic.StoreInt64(&c.failures, 0)
	}

	// Open breaker if failure threshold is reached
	if atomic.AddInt64(&c.failures, 1) >= int64(max(1, config.RetryCBFailures())) {
		cooldown := time.Duration(max(1, config.RetryCBCooldownSec())) * time.Second
		atomic.StoreInt64(&c.openUntil, time.Now().Add(cooldown).UnixNano())
	}
}

// onSuccess resets the failure count on successful request
// Helps keep the circuit breaker closed during healthy operation
func (c *circuitBreaker) onSuccess() {
	atomic.StoreInt64(&c.failures, 0)
}

// httpClient provides HTTP functionality with reliability features
// Includes circuit breaker, rate limiting, and structured error handling
type httpClient struct {
	logger    log.Logger      // Logger for request tracking
	client    *http.Client    // Underlying HTTP client
	rateLimit *time.Ticker    // Rate limiter for request throttling
	cbState   *circuitBreaker // Circuit breaker for failure protection
}

// newClient creates a new HTTP client with configured transport and reliability features
// Sets up rate limiting, circuit breaker, and optimized connection pooling
func newClient(logger log.Logger) *httpClient {
	hc := new(httpClient)
	hc.logger = logger

	// Configure HTTP client with optimized transport settings
	hc.client = &http.Client{
		Timeout: time.Duration(config.HTTPTimeoutSec()) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:          config.HTTPMaxIdleConns(),
			MaxIdleConnsPerHost:   config.HTTPMaxIdlePerHost(),
			IdleConnTimeout:       time.Duration(config.HTTPIdleConnTimeoutSec()) * time.Second,
			MaxConnsPerHost:       config.HTTPMaxConnsPerHost(),
			WriteBufferSize:       config.HTTPWriteBufferKB(),
			ReadBufferSize:        config.HTTPReadBufferKB(),
			DisableKeepAlives:     config.HTTPDisableKeepAlives(),
			DisableCompression:    config.HTTPDisableCompression(),
			ForceAttemptHTTP2:     config.HTTPForceAttemptHTTP2(),
			TLSHandshakeTimeout:   time.Duration(config.HTTPTLSHandshakeTimeoutSec()) * time.Second,
			ExpectContinueTimeout: time.Duration(config.HTTPEExpectContinueTimeoutSec()) * time.Second,
		},
	}

	// Setup rate limiting and circuit breaker
	rps := max(1, config.HTTPRequestsPerSec())
	hc.rateLimit = time.NewTicker(time.Second / time.Duration(rps))
	hc.cbState = newCircuitBreaker()

	return hc
}

// fetchRawData retrieves data from external API with retry and circuit breaker logic
// Implements comprehensive error handling for different HTTP status codes
func (hc *httpClient) fetchRawData(url string) ([]byte, error) {
	maxAttempts := max(1, config.RetryMaxAttempts())
	cooldown := time.Duration(max(1, config.RetryCBCooldownSec())) * time.Second
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if hc.rateLimit != nil {
			<-hc.rateLimit.C
		}

		if !hc.cbState.allow() {
			hc.logger.Debug("circuit breaker is open", "cooldown", cooldown)
			time.Sleep(cooldown)
			continue
		}

		if 0 < attempt {
			retryDelay := time.Duration(1<<attempt) * time.Second
			hc.logger.Debug("retry", "delay", retryDelay)
			time.Sleep(retryDelay)
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("User-Agent", "Guru-V2-Oracle/1.0")
		req.Header.Set("Accept", "application/json")

		res, err := hc.client.Do(req)
		if err != nil {
			hc.cbState.onFailure()
			continue
		}

		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return nil, err
		}

		switch {
		case res.StatusCode == http.StatusOK:
			hc.cbState.onSuccess()
			return body, nil

		case 500 <= res.StatusCode:
			// 서버 에러 - 재시도 가능
			hc.cbState.onFailure()
			continue

		case res.StatusCode == http.StatusRequestTimeout ||
			res.StatusCode == http.StatusTooManyRequests ||
			res.StatusCode == http.StatusConflict:
			// 일시적 에러 - 재시도 가능
			hc.cbState.onFailure()
			continue

		default:
			// 클라이언트 에러 - 재시도 불가
			return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, string(body))
		}
	}

	return nil, fmt.Errorf("failed to fetch raw data after %d attempts", maxAttempts)
}

// parseRawData converts raw JSON bytes into a structured map
// Handles both JSON objects and arrays, returning the first object for arrays
func (hc *httpClient) parseRawData(rawData []byte) (map[string]any, error) {
	var result any

	// Parse JSON into generic interface
	if err := json.Unmarshal(rawData, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Handle different JSON root types
	switch v := result.(type) {
	case map[string]any:
		// Direct object, return as-is
		return v, nil
	case []any:
		// Array, return first element if it's an object
		if len(v) == 0 {
			return nil, fmt.Errorf("empty JSON array")
		}
		if obj, ok := v[0].(map[string]any); ok {
			return obj, nil
		}
		return nil, fmt.Errorf("first array element is not a JSON object")
	default:
		return nil, fmt.Errorf("JSON must be object or array, got %T", result)
	}
}

// extractDataByPath navigates through JSON structure using dot-separated path
// Supports both object property access and array index access
func (hc *httpClient) extractDataByPath(data map[string]any, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Split the path by dots to get individual components
	pathParts := strings.Split(path, ".")

	current := any(data)
	for _, part := range pathParts {
		switch v := current.(type) {
		case map[string]any:
			// Navigate through object properties
			if val, exists := v[part]; exists {
				current = val
			} else {
				return "", fmt.Errorf("key '%s' not found in path %s", part, path)
			}
		case []any:
			// Navigate through array indices
			if index, parseErr := parseArrayIndex(part); parseErr == nil {
				if index >= 0 && index < len(v) {
					current = v[index]
				} else {
					return "", fmt.Errorf("array index %d out of bounds (length: %d)", index, len(v))
				}
			} else {
				return "", fmt.Errorf("invalid array index '%s': %v", part, parseErr)
			}
		default:
			return "", fmt.Errorf("cannot traverse '%s' in type %T", part, current)
		}
	}

	// Convert final value to string representation
	return fmt.Sprintf("%v", current), nil
}

// parseArrayIndex converts string to array index with validation
// Ensures index is non-negative and properly formatted
func parseArrayIndex(s string) (int, error) {
	if s == "" {
		return -1, fmt.Errorf("array index cannot be empty")
	}

	// Parse string to integer
	index, err := strconv.Atoi(s)
	if err != nil {
		return -1, fmt.Errorf("invalid array index '%s': %w", s, err)
	}

	// Validate index is non-negative
	if index < 0 {
		return -1, fmt.Errorf("negative array index not allowed: %d", index)
	}

	return index, nil
}
