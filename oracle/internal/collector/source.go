package collector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

type SourceClient struct {
	client         *http.Client
	responseLimit  int64
	maxAttempts    uint32
	initialBackoff time.Duration
	maxBackoff     time.Duration
	requestTimeout time.Duration
}

type fetchError struct {
	retryable bool
	reason    string
}

func (e *fetchError) Error() string { return e.reason }

func NewSourceClient(policy domain.CollectorPolicy) (*SourceClient, error) {
	roots, err := trustedRootPool()
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            (&net.Dialer{Timeout: policy.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           int(policy.MaxConcurrency),
		MaxIdleConnsPerHost:    8,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    policy.TLSHandshakeTimeout,
		ResponseHeaderTimeout:  policy.ResponseHeaderTimeout,
		MaxResponseHeaderBytes: 64 << 10,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
	}
	maxRedirects := int(policy.MaxRedirects)
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return &fetchError{reason: "redirect limit exceeded"}
			}
			if err := domain.ValidateSourceURL(request.URL.String()); err != nil {
				return &fetchError{reason: "redirect URL rejected"}
			}
			return nil
		},
	}
	return &SourceClient{
		client:         client,
		responseLimit:  int64(policy.SourceResponseBytes),
		maxAttempts:    policy.MaxAttempts,
		initialBackoff: policy.RetryInitialBackoff,
		maxBackoff:     policy.RetryMaxBackoff,
		requestTimeout: policy.RequestTimeout,
	}, nil
}

func (c *SourceClient) CloseIdleConnections() {
	c.client.CloseIdleConnections()
}

func (c *SourceClient) Fetch(ctx context.Context, source domain.SourcePlan) (sdkmath.LegacyDec, error) {
	backoff := c.initialBackoff
	for attempt := uint32(1); attempt <= c.maxAttempts; attempt++ {
		value, failure := c.fetchOnce(ctx, source)
		if failure == nil {
			return value, nil
		}
		if !failure.retryable || attempt == c.maxAttempts {
			return sdkmath.LegacyDec{}, failure
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return sdkmath.LegacyDec{}, ctx.Err()
		case <-timer.C:
		}
		if backoff < c.maxBackoff {
			backoff *= 2
			if backoff > c.maxBackoff {
				backoff = c.maxBackoff
			}
		}
	}
	return sdkmath.LegacyDec{}, errors.New("source attempts exhausted")
}

func (c *SourceClient) fetchOnce(ctx context.Context, source domain.SourcePlan) (sdkmath.LegacyDec, *fetchError) {
	timeout := c.requestTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return sdkmath.LegacyDec{}, &fetchError{reason: "cycle deadline reached"}
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, source.URL, nil)
	if err != nil {
		return sdkmath.LegacyDec{}, &fetchError{reason: "request construction failed"}
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return sdkmath.LegacyDec{}, classifyTransportError(ctx, attemptCtx, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		retryable := response.StatusCode == http.StatusRequestTimeout ||
			response.StatusCode == http.StatusTooManyRequests ||
			(response.StatusCode >= 500 && response.StatusCode <= 599)
		return sdkmath.LegacyDec{}, &fetchError{retryable: retryable, reason: "HTTP status rejected"}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, c.responseLimit+1))
	if err != nil {
		if attemptCtx.Err() != nil {
			return sdkmath.LegacyDec{}, &fetchError{retryable: ctx.Err() == nil, reason: "response read timed out"}
		}
		return sdkmath.LegacyDec{}, &fetchError{retryable: true, reason: "response read failed"}
	}
	if int64(len(body)) > c.responseLimit {
		return sdkmath.LegacyDec{}, &fetchError{reason: "response body exceeds limit"}
	}
	if failure := contextFailure(ctx, attemptCtx); failure != nil {
		return sdkmath.LegacyDec{}, failure
	}
	text, err := extractJSONNumericText(attemptCtx, body, source.JSONPointer)
	if err != nil {
		if failure := contextFailure(ctx, attemptCtx); failure != nil {
			return sdkmath.LegacyDec{}, failure
		}
		switch {
		case errors.Is(err, errInvalidSourceUTF8):
			return sdkmath.LegacyDec{}, &fetchError{reason: "response body is not valid UTF-8"}
		case errors.Is(err, errJSONPointerUnresolved):
			return sdkmath.LegacyDec{}, &fetchError{reason: "JSON Pointer did not resolve"}
		case errors.Is(err, errJSONPointerNotNumeric):
			return sdkmath.LegacyDec{}, &fetchError{reason: "JSON Pointer value is not numeric text"}
		case errors.Is(err, errJSONNumericTokenTooLong):
			return sdkmath.LegacyDec{}, &fetchError{reason: "numeric value rejected"}
		default:
			return sdkmath.LegacyDec{}, &fetchError{reason: "invalid JSON response"}
		}
	}
	normalized, err := domain.NormalizeDecimal(text)
	if err != nil {
		return sdkmath.LegacyDec{}, &fetchError{reason: "numeric value rejected"}
	}
	decimal, err := domain.ParseCanonicalDecimal(normalized)
	if err != nil {
		return sdkmath.LegacyDec{}, &fetchError{reason: "numeric value out of range"}
	}
	if failure := contextFailure(ctx, attemptCtx); failure != nil {
		return sdkmath.LegacyDec{}, failure
	}
	return decimal, nil
}

func contextFailure(parentCtx, attemptCtx context.Context) *fetchError {
	if parentCtx.Err() != nil {
		return &fetchError{reason: "request cancelled"}
	}
	if attemptCtx.Err() != nil {
		return &fetchError{retryable: true, reason: "request timed out"}
	}
	return nil
}

func classifyTransportError(parentCtx, attemptCtx context.Context, err error) *fetchError {
	if parentCtx.Err() != nil {
		return &fetchError{retryable: false, reason: "request cancelled"}
	}
	if attemptCtx.Err() != nil {
		return &fetchError{retryable: true, reason: "request timed out"}
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		err = urlError.Err
	}
	var certificateError *tls.CertificateVerificationError
	if errors.As(err, &certificateError) {
		return &fetchError{reason: "TLS verification failed"}
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		if dnsError.IsNotFound {
			return &fetchError{reason: "DNS name not found"}
		}
		return &fetchError{retryable: true, reason: "temporary DNS failure"}
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return &fetchError{retryable: true, reason: "transport failure"}
	}
	var policyError *fetchError
	if errors.As(err, &policyError) {
		return policyError
	}
	return &fetchError{retryable: true, reason: "transport failure"}
}

// trustedRootPool explicitly honors SSL_CERT_FILE on every supported
// platform. Go's Darwin verifier does not consume it itself.
func trustedRootPool() (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system trust roots: %w", err)
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	path := os.Getenv("SSL_CERT_FILE")
	if path == "" {
		return roots, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open SSL_CERT_FILE: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat SSL_CERT_FILE: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("SSL_CERT_FILE must be a regular file no larger than 1 MiB")
	}
	pemBytes, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read SSL_CERT_FILE: %w", err)
	}
	if len(pemBytes) > 1<<20 || !roots.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("SSL_CERT_FILE does not contain a valid bounded PEM certificate")
	}
	return roots, nil
}
