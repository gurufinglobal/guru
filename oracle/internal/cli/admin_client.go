package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/service"
)

const (
	adminBodyLimit   = 1 << 20
	adminHeaderLimit = 64 << 10
)

type adminProtocolError struct {
	err error
}

func (e *adminProtocolError) Error() string { return e.err.Error() }
func (e *adminProtocolError) Unwrap() error { return e.err }

func asAdminProtocolError(err error) error {
	return &adminProtocolError{err: err}
}

func isAdminProtocolError(err error) bool {
	var protocolError *adminProtocolError
	return errors.As(err, &protocolError)
}

type adminTransportError struct {
	err error
}

func (e *adminTransportError) Error() string { return e.err.Error() }
func (e *adminTransportError) Unwrap() error { return e.err }

func asAdminTransportError(err error) error {
	return &adminTransportError{err: err}
}

func isAdminTransportError(err error) bool {
	var transportError *adminTransportError
	return errors.As(err, &transportError)
}

func fetchAdmin(ctx context.Context, socketPath, requestPath string) ([]byte, int, error) {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			connection, err := dialer.DialContext(ctx, "unix", socketPath)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return nil, err
				}
				return nil, asAdminTransportError(err)
			}
			return connection, nil
		},
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: adminHeaderLimit,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+requestPath, nil)
	if err != nil {
		return nil, 0, err
	}
	response, err := client.Do(request)
	if err != nil {
		if strings.Contains(err.Error(), "server response headers exceeded") {
			return nil, 0, asAdminProtocolError(errors.New("admin response headers exceed limit"))
		}
		if errors.Is(err, context.Canceled) {
			return nil, 0, err
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return nil, 0, asAdminTransportError(err)
		}
		if isDefiniteAdminTransportFailure(err) {
			return nil, 0, asAdminTransportError(err)
		}
		// Only positively identified transport failures permit offline fallback.
		// Every other HTTP client failure is a protocol error so malformed or
		// truncated responses fail closed.
		return nil, 0, asAdminProtocolError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if !service.ValidateAdminContentType(response.Header.Get("Content-Type")) {
		return nil, response.StatusCode, asAdminProtocolError(errors.New("admin response content type is invalid"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, adminBodyLimit+1))
	if err != nil {
		return nil, response.StatusCode, asAdminProtocolError(fmt.Errorf("read admin response: %w", err))
	}
	if len(body) > adminBodyLimit {
		return nil, response.StatusCode, asAdminProtocolError(errors.New("admin response exceeds limit"))
	}
	if len(body) == 0 {
		return nil, response.StatusCode, asAdminProtocolError(errors.New("admin response is empty"))
	}
	return body, response.StatusCode, nil
}

func isDefiniteAdminTransportFailure(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	if errorChainContainsExact(err, "http: server closed idle connection") {
		return true
	}
	var operationError *net.OpError
	return errors.As(err, &operationError)
}

func errorChainContainsExact(err error, message string) bool {
	for current := err; current != nil; current = errors.Unwrap(current) {
		if current.Error() == message {
			return true
		}
	}
	return false
}

func decodeSuccess[T any](body []byte, command string) (service.SuccessEnvelope[T], error) {
	var envelope service.SuccessEnvelope[T]
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return envelope, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return envelope, errors.New("admin response has trailing JSON")
	}
	if envelope.SchemaVersion != 1 || envelope.Command != command {
		return envelope, errors.New("admin response envelope is incompatible")
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.GeneratedAt); err != nil {
		return envelope, errors.New("admin response timestamp is invalid")
	}
	return envelope, nil
}

func decodeAdminError(body []byte) error {
	var envelope service.ErrorEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return errors.New("admin returned a non-conforming error")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("admin error response has trailing JSON")
	}
	if envelope.SchemaVersion != 1 ||
		(envelope.Command != "status" && envelope.Command != "history" && envelope.Command != "admin") ||
		envelope.Error.Code == "" || len(envelope.Error.Message) > 512 {
		return errors.New("admin returned an invalid error envelope")
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.GeneratedAt); err != nil {
		return errors.New("admin error response timestamp is invalid")
	}
	return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
}

func writeRawJSON(output io.Writer, body []byte) error {
	_, err := output.Write(append(append([]byte(nil), body...), '\n'))
	return err
}

func boundedMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}
