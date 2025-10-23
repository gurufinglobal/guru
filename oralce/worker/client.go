package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/log"
	"github.com/gurufinglobal/guru/v2/oralce/config"
)

const (
	// maxResponseSize limits the maximum size of HTTP response bodies to prevent memory exhaustion.
	// Oracle typically fetches JSON data (a few KB to hundreds of KB), so 10MB is generous.
	maxResponseSize = 10 * 1024 * 1024 // 10MB

	// maxErrorBodyPreview limits how much of the response body is included in error messages
	// to prevent log flooding and disk space exhaustion.
	maxErrorBodyPreview = 500 // 500 bytes
)

type httpClient struct {
	logger log.Logger
	client *http.Client
}

func newHTTPClient(logger log.Logger) *httpClient {
	hc := new(httpClient)
	hc.logger = logger

	hc.client = &http.Client{
		Timeout: time.Duration(30) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:          1000,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       time.Duration(90) * time.Second,
			MaxConnsPerHost:       100,
			WriteBufferSize:       32 * 1024,
			ReadBufferSize:        32 * 1024,
			DisableKeepAlives:     false,
			DisableCompression:    false,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   time.Duration(10) * time.Second,
			ExpectContinueTimeout: time.Duration(1) * time.Second,
		},
	}

	return hc
}

// fetchRawData retrieves bytes from an external endpoint with bounded retries.
func (hc *httpClient) fetchRawData(url string) ([]byte, error) {
	maxAttempts := max(1, config.RetryMaxAttempts())
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if 0 < attempt {
			retryDelay := time.Duration(1<<(attempt-1)) * time.Second
			actualDelay := min(retryDelay, config.RetryMaxDelaySec())
			hc.logger.Debug("retrying HTTP request",
				"url", url,
				"attempt", attempt+1,
				"max_attempts", maxAttempts,
				"delay_seconds", actualDelay.Seconds())
			time.Sleep(actualDelay)
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		req.Header.Set("User-Agent", "Guru-V2-Oracle/1.0")
		req.Header.Set("Accept", "application/json")

		res, err := hc.client.Do(req)
		if err != nil {
			lastErr = err
			hc.logger.Warn("HTTP request failed",
				"url", url,
				"attempt", attempt+1,
				"max_attempts", maxAttempts,
				"error", err)
			continue
		}

		// Check Content-Length header if present
		if res.ContentLength > maxResponseSize {
			res.Body.Close()
			return nil, fmt.Errorf("response too large: Content-Length=%d bytes (max: %d)", res.ContentLength, maxResponseSize)
		}

		// Use LimitReader to enforce size limit during read
		limitedReader := io.LimitReader(res.Body, maxResponseSize+1)
		body, err := io.ReadAll(limitedReader)
		res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Verify actual size (handles missing/incorrect Content-Length)
		if len(body) > maxResponseSize {
			return nil, fmt.Errorf("response exceeded size limit: %d bytes (max: %d)", len(body), maxResponseSize)
		}

		switch {
		case res.StatusCode == http.StatusOK:
			if attempt > 0 {
				hc.logger.Info("HTTP request succeeded after retry",
					"url", url,
					"attempts", attempt+1)
			}
			return body, nil

		case 500 <= res.StatusCode:
			lastErr = fmt.Errorf("HTTP %d: %s", res.StatusCode, string(body))
			hc.logger.Warn("server error, will retry if attempts remain",
				"url", url,
				"status_code", res.StatusCode,
				"attempt", attempt+1,
				"max_attempts", maxAttempts,
				"response_preview", truncateString(string(body), 100))
			continue

		case res.StatusCode == http.StatusRequestTimeout ||
			res.StatusCode == http.StatusTooManyRequests ||
			res.StatusCode == http.StatusConflict:
			lastErr = fmt.Errorf("HTTP %d: %s", res.StatusCode, string(body))
			hc.logger.Warn("retryable HTTP error",
				"url", url,
				"status_code", res.StatusCode,
				"attempt", attempt+1,
				"max_attempts", maxAttempts)
			continue

		default:
			// Truncate body in error message to prevent log flooding
			preview := truncateForError(body, maxErrorBodyPreview)
			return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, preview)
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch raw data after %d attempts, last error: %w", maxAttempts, lastErr)
	}
	return nil, fmt.Errorf("failed to fetch raw data after %d attempts", maxAttempts)
}

// parseRawData parses JSON bytes and returns a map for object or first element of array.
func (hc *httpClient) parseRawData(rawData []byte) (map[string]any, error) {
	var result any

	if err := json.Unmarshal(rawData, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	switch v := result.(type) {
	case map[string]any:
		return v, nil
	case []any:
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

// extractDataByPath navigates a JSON-like map using dot notation and array indices.
func (hc *httpClient) extractDataByPath(data map[string]any, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	pathParts := strings.Split(path, ".")

	current := any(data)
	for _, part := range pathParts {
		switch v := current.(type) {
		case map[string]any:
			if val, exists := v[part]; exists {
				current = val
			} else {
				return "", fmt.Errorf("key '%s' not found in path %s", part, path)
			}
		case []any:
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

	return fmt.Sprintf("%v", current), nil
}

// parseArrayIndex converts a path segment into a non-negative array index.
func parseArrayIndex(s string) (int, error) {
	if s == "" {
		return -1, fmt.Errorf("array index cannot be empty")
	}

	index, err := strconv.Atoi(s)
	if err != nil {
		return -1, fmt.Errorf("invalid array index '%s': %w", s, err)
	}

	if index < 0 {
		return -1, fmt.Errorf("negative array index not allowed: %d", index)
	}

	return index, nil
}

// truncateForError limits body size for error messages and provides clear indication of truncation.
// This prevents log flooding and disk space exhaustion when large or malicious responses occur.
func truncateForError(body []byte, maxLen int) string {
	if len(body) == 0 {
		return "(empty response)"
	}

	if len(body) <= maxLen {
		return string(body)
	}

	return string(body[:maxLen]) + fmt.Sprintf("... (truncated, %d more bytes)", len(body)-maxLen)
}

// truncateString truncates a string to maxLen characters, appending "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
