package cli

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchAdminClassifiesInvalidResponsesAsProtocolErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "wrong content type",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/plain")
				_, _ = writer.Write([]byte("{}"))
			},
		},
		{
			name: "empty body",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "oversized body",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				_, _ = writer.Write([]byte(strings.Repeat("x", adminBodyLimit+1)))
			},
		},
		{
			name: "oversized headers",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.Header().Set("X-Oversized", strings.Repeat("x", adminHeaderLimit))
				_, _ = writer.Write([]byte("{}"))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			socket := shortAdminSocket(t)
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: test.handler}
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			t.Cleanup(func() {
				_ = server.Close()
				<-done
			})

			_, _, err = fetchAdmin(context.Background(), socket, "/v1/status")
			if err == nil || !isAdminProtocolError(err) {
				t.Fatalf("fetchAdmin error = %v, want protocol error", err)
			}
			if code := adminFailureCode(err); code != "protocol_error" {
				t.Fatalf("failure code = %q", code)
			}
		})
	}
}

func TestAdminConnectionFailureRemainsDaemonUnavailable(t *testing.T) {
	_, _, err := fetchAdmin(context.Background(), filepath.Join(shortAdminDirectory(t), "missing.sock"), "/v1/status")
	if err == nil {
		t.Fatal("fetchAdmin unexpectedly succeeded")
	}
	if code := adminFailureCode(err); code != "daemon_unavailable" {
		t.Fatalf("failure code = %q", code)
	}
}

func TestFetchAdminClassifiesOnlyDefinitePostConnectTransportFailures(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		wantTransport bool
	}{
		{name: "closed without response", wantTransport: true},
		{name: "malformed HTTP", response: "NOT HTTP\r\n\r\n", wantTransport: false},
		{name: "malformed HTTP containing sentinel text", response: "server closed idle connection\r\n\r\n", wantTransport: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			socket := shortAdminSocket(t)
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					done <- acceptErr
					return
				}
				request, readErr := http.ReadRequest(bufio.NewReader(connection))
				if request != nil {
					_ = request.Body.Close()
				}
				if readErr == nil && test.response != "" {
					_, acceptErr = connection.Write([]byte(test.response))
				}
				closeErr := connection.Close()
				done <- errors.Join(readErr, acceptErr, closeErr)
			}()

			_, _, fetchErr := fetchAdmin(context.Background(), socket, "/v1/status")
			_ = listener.Close()
			if serverErr := <-done; serverErr != nil {
				t.Fatalf("raw server: %v", serverErr)
			}
			if fetchErr == nil {
				t.Fatal("fetchAdmin unexpectedly succeeded")
			}
			if got := isAdminTransportError(fetchErr); got != test.wantTransport {
				t.Fatalf("transport classification = %t, want %t: %v", got, test.wantTransport, fetchErr)
			}
			wantCode := "protocol_error"
			if test.wantTransport {
				wantCode = "daemon_unavailable"
			}
			if got := adminFailureCode(fetchErr); got != wantCode {
				t.Fatalf("failure code = %q, want %q: %v", got, wantCode, fetchErr)
			}
		})
	}
}

func TestFetchAdminCancellationDoesNotEnableOfflineFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := fetchAdmin(ctx, filepath.Join(shortAdminDirectory(t), "missing.sock"), "/v1/status")
	if !errors.Is(err, context.Canceled) || isAdminTransportError(err) {
		t.Fatalf("cancellation classification = %v", err)
	}
}

func shortAdminSocket(t *testing.T) string {
	t.Helper()
	return filepath.Join(shortAdminDirectory(t), "a.sock")
}

func shortAdminDirectory(t *testing.T) string {
	t.Helper()
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(tempRoot, "oa-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}
