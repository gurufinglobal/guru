package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/storage"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCanonicalHistoryPageSize(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		value   string
		want    uint32
		wantErr bool
	}{
		{name: "default", want: 30},
		{name: "minimum", value: "1", want: 1},
		{name: "maximum", value: "50", want: 50},
		{name: "zero", value: "0", wantErr: true},
		{name: "above maximum", value: "51", wantErr: true},
		{name: "leading zero", value: "01", wantErr: true},
		{name: "positive sign", value: "+1", wantErr: true},
		{name: "negative sign", value: "-1", wantErr: true},
		{name: "nonnumeric", value: "one", wantErr: true},
		{name: "uint32 overflow", value: "4294967296", wantErr: true},
		{name: "larger overflow", value: strings.Repeat("9", 128), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseCanonicalHistoryPageSize(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseCanonicalHistoryPageSize(%q) = %d, %v, wantErr %t", test.value, got, err, test.wantErr)
			}
			if !test.wantErr && got != test.want {
				t.Fatalf("parseCanonicalHistoryPageSize(%q) = %d, want %d", test.value, got, test.want)
			}
			if test.wantErr && err.Error() != "page_size must be a canonical decimal from 1 to 50" {
				t.Fatalf("parseCanonicalHistoryPageSize(%q) error = %v", test.value, err)
			}
		})
	}
}

func TestAdminHistoryPageSizeBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Now()
	pair, catalog, latest := serviceFixture(t, now.UTC())
	state := NewState(pair, catalog, latest, now)

	for _, test := range []struct {
		name     string
		query    string
		wantPage uint32
	}{
		{name: "default", wantPage: 30},
		{name: "explicit empty", query: "&page_size=", wantPage: 30},
		{name: "minimum", query: "&page_size=1", wantPage: 1},
		{name: "maximum", query: "&page_size=50", wantPage: 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingPageHistoryStore{catalog: catalog}
			server := NewAdminServer(state, store)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(
				http.MethodGet,
				"/v1/history?symbol=BTC%2FUSD"+test.query,
				nil,
			))
			if recorder.Code != http.StatusOK {
				t.Fatalf("history status = %d body=%s", recorder.Code, recorder.Body)
			}
			if store.pageSize != test.wantPage {
				t.Fatalf("history page size = %d, want %d", store.pageSize, test.wantPage)
			}
		})
	}

	for _, value := range []string{"0", "51", "01", "+1", "-1", "one", "4294967296"} {
		t.Run("invalid "+value, func(t *testing.T) {
			server := NewAdminServer(state, &recordingPageHistoryStore{catalog: catalog})
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(
				http.MethodGet,
				"/v1/history?symbol=BTC%2FUSD&page_size="+value,
				nil,
			))
			assertAdminErrorResponse(t, recorder, http.StatusBadRequest, "history", "invalid_page_size")
			if !strings.Contains(recorder.Body.String(), "History page_size must be from 1 to 50.") {
				t.Fatalf("history page-size error body = %s", recorder.Body)
			}
		})
	}
}

func TestConsumerRequestBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Now()
	pair, catalog, latest := serviceFixture(t, now.UTC())
	state := NewState(pair, catalog, latest, now)
	server := NewConsumerServer(state, 64<<10, 1<<20)

	if _, err := server.GetAggregates(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil request error = %v", err)
	}
	tooMany := make([]string, maxConsumerSymbols+1)
	for i := range tooMany {
		tooMany[i] = "BTC/USD"
	}
	tests := []struct {
		name    string
		symbols []string
		want    codes.Code
	}{
		{name: "unsorted", symbols: []string{"ETH/USD", "BTC/USD"}, want: codes.InvalidArgument},
		{name: "duplicate", symbols: []string{"BTC/USD", "BTC/USD"}, want: codes.InvalidArgument},
		{name: "over symbol limit", symbols: tooMany, want: codes.ResourceExhausted},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := server.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
				Symbols: test.symbols,
			})
			if status.Code(err) != test.want {
				t.Fatalf("error = %v, want code %s", err, test.want)
			}
		})
	}

	requestLimited := NewConsumerServer(state, 1, 1<<20)
	if _, err := requestLimited.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
		Symbols: []string{"BTC/USD"},
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("request-size error = %v", err)
	}
	responseLimited := NewConsumerServer(state, 64<<10, 1)
	if _, err := responseLimited.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
		Symbols: []string{"BTC/USD"},
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("response-size error = %v", err)
	}
}

func TestConsumerReturnsFreshSubsetForPartialOmissions(t *testing.T) {
	t.Parallel()
	now := time.Now()
	pair, catalog, latest := serviceFixture(t, now.UTC())
	server := NewConsumerServer(NewState(pair, catalog, latest, now), 64<<10, 1<<20)
	response, err := server.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
		Symbols: []string{"BTC/USD", "ETH/USD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Symbol != "BTC/USD" {
		t.Fatalf("partial response = %#v", response.Results)
	}
}

func TestAdminRequestBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Now()
	pair, catalog, latest := serviceFixture(t, now.UTC())
	server := NewAdminServer(NewState(pair, catalog, latest, now), nil)
	tests := []struct {
		name        string
		method      string
		target      string
		body        string
		transfer    bool
		wantStatus  int
		wantCommand string
		wantCode    string
		wantAllow   string
	}{
		{
			name:        "wrong route",
			method:      http.MethodGet,
			target:      "/v1/unknown",
			wantStatus:  http.StatusNotFound,
			wantCommand: "admin",
			wantCode:    "not_found",
		},
		{
			name:        "wrong method",
			method:      http.MethodPost,
			target:      "/v1/status",
			wantStatus:  http.StatusMethodNotAllowed,
			wantCommand: "status",
			wantCode:    "method_not_allowed",
			wantAllow:   http.MethodGet,
		},
		{
			name:        "content-length body",
			method:      http.MethodGet,
			target:      "/v1/status",
			body:        "x",
			wantStatus:  http.StatusBadRequest,
			wantCommand: "status",
			wantCode:    "invalid_request",
		},
		{
			name:        "transfer-encoded body",
			method:      http.MethodGet,
			target:      "/v1/status",
			transfer:    true,
			wantStatus:  http.StatusBadRequest,
			wantCommand: "status",
			wantCode:    "invalid_request",
		},
		{
			name:        "unknown history query",
			method:      http.MethodGet,
			target:      "/v1/history?symbol=BTC%2FUSD&unknown=x",
			wantStatus:  http.StatusBadRequest,
			wantCommand: "history",
			wantCode:    "invalid_request",
		},
		{
			name:        "duplicate history query",
			method:      http.MethodGet,
			target:      "/v1/history?symbol=BTC%2FUSD&symbol=ETH%2FUSD",
			wantStatus:  http.StatusBadRequest,
			wantCommand: "history",
			wantCode:    "invalid_request",
		},
		{
			name:        "request URI limit",
			method:      http.MethodGet,
			target:      "/v1/status?" + strings.Repeat("x", adminURILimit),
			wantStatus:  http.StatusRequestURITooLong,
			wantCommand: "status",
			wantCode:    "request_uri_too_long",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var body *strings.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			} else {
				body = strings.NewReader("")
			}
			request := httptest.NewRequest(test.method, test.target, body)
			if test.body == "" {
				request.Body = http.NoBody
				request.ContentLength = 0
			}
			if test.transfer {
				request.TransferEncoding = []string{"chunked"}
			}
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			assertAdminErrorResponse(
				t,
				recorder,
				test.wantStatus,
				test.wantCommand,
				test.wantCode,
			)
			if recorder.Header().Get("Allow") != test.wantAllow {
				t.Fatalf("Allow = %q, want %q", recorder.Header().Get("Allow"), test.wantAllow)
			}
		})
	}
}

func TestAdminResponseBound(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writeAdminJSON(
		recorder,
		http.StatusOK,
		map[string]string{"value": strings.Repeat("x", adminResponseLimit)},
		"status",
	)
	assertAdminErrorResponse(
		t,
		recorder,
		http.StatusInternalServerError,
		"status",
		"response_too_large",
	)
}

func assertAdminErrorResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	wantStatus int,
	wantCommand,
	wantCode string,
) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body)
	}
	if !ValidateAdminContentType(recorder.Header().Get("Content-Type")) {
		t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
	}
	var envelope ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.SchemaVersion != 1 || envelope.Command != wantCommand ||
		envelope.Error.Code != wantCode || envelope.Error.Message == "" {
		t.Fatalf("error envelope = %#v", envelope)
	}
}

type recordingPageHistoryStore struct {
	catalog  storage.Catalog
	pageSize uint32
}

func (s *recordingPageHistoryStore) History(_ string, pageSize uint32, _ []byte) (storage.HistoryPage, error) {
	s.pageSize = pageSize
	return storage.HistoryPage{}, nil
}

func (s *recordingPageHistoryStore) Catalog() storage.Catalog {
	return s.catalog
}
