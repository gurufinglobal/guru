package service

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

const (
	adminResponseLimit = 1 << 20
	adminURILimit      = 4096
)

type SuccessEnvelope[T any] struct {
	SchemaVersion uint32 `json:"schema_version"`
	Command       string `json:"command"`
	GeneratedAt   string `json:"generated_at"`
	Data          T      `json:"data"`
}

type ErrorEnvelope struct {
	SchemaVersion uint32     `json:"schema_version"`
	Command       string     `json:"command"`
	GeneratedAt   string     `json:"generated_at"`
	Error         ErrorValue `json:"error"`
}

type ErrorValue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type HistoryData struct {
	Symbol            string          `json:"symbol"`
	HighWaterSequence string          `json:"high_water_sequence"`
	Records           []HistoryRecord `json:"records"`
	NextPageKey       *string         `json:"next_page_key"`
}

type HistoryRecord struct {
	Value                 string   `json:"value"`
	Sequence              string   `json:"sequence"`
	ActivationGeneration  string   `json:"activation_generation"`
	CycleStartedAt        string   `json:"cycle_started_at"`
	CollectedAt           string   `json:"collected_at"`
	ConfiguredSourceCount uint32   `json:"configured_source_count"`
	SuccessfulSourceCount uint32   `json:"successful_source_count"`
	ContributorIDs        []string `json:"contributor_ids"`
	FeedPlanFingerprint   string   `json:"feed_plan_fingerprint"`
	Provenance            string   `json:"provenance"`
}

type AdminServer struct {
	state *State
	store historyStore
	fatal func(error)

	fatalOnce sync.Once
}

type historyStore interface {
	History(symbol string, pageSize uint32, token []byte) (storage.HistoryPage, error)
	Catalog() storage.Catalog
}

func NewAdminServer(state *State, store historyStore) *AdminServer {
	return &AdminServer{state: state, store: store}
}

func newAdminServerWithFatal(state *State, store historyStore, fatal func(error)) *AdminServer {
	return &AdminServer{state: state, store: store, fatal: fatal}
}

func (s *AdminServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	command := commandForPath(request.URL.Path)
	defer func() {
		if recover() != nil {
			writeAdminError(
				writer,
				http.StatusConflict,
				command,
				"storage_unavailable",
				"Admin data is unavailable.",
			)
			s.signalAdminFatal()
		}
	}()
	if len(request.RequestURI) > adminURILimit {
		writeAdminError(writer, http.StatusRequestURITooLong, command, "request_uri_too_long", "Request URI exceeds 4096 bytes.")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeAdminError(writer, http.StatusMethodNotAllowed, command, "method_not_allowed", "Only GET is allowed.")
		return
	}
	if request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
		writeAdminError(writer, http.StatusBadRequest, command, "invalid_request", "Request body is not allowed.")
		return
	}
	switch request.URL.Path {
	case "/v1/status":
		s.handleStatus(writer, request)
	case "/v1/history":
		s.handleHistory(writer, request)
	default:
		writeAdminError(writer, http.StatusNotFound, command, "not_found", "Admin route was not found.")
	}
}

func (s *AdminServer) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeAdminError(writer, http.StatusBadRequest, "status", "invalid_request", "Status does not accept query parameters.")
		return
	}
	data, observedAt := s.state.Status()
	envelope := SuccessEnvelope[StatusData]{
		SchemaVersion: 1,
		Command:       "status",
		GeneratedAt:   observedAt.UTC().Format(time.RFC3339Nano),
		Data:          data,
	}
	writeAdminJSON(writer, http.StatusOK, envelope, "status")
}

func (s *AdminServer) handleHistory(writer http.ResponseWriter, request *http.Request) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		writeAdminError(writer, http.StatusBadRequest, "history", "invalid_request", "History query is invalid.")
		return
	}
	for key, values := range query {
		if key != "symbol" && key != "page_size" && key != "page_key" {
			writeAdminError(writer, http.StatusBadRequest, "history", "invalid_request", "History query contains an unknown parameter.")
			return
		}
		if len(values) != 1 {
			writeAdminError(writer, http.StatusBadRequest, "history", "invalid_request", "History query parameters must be unique.")
			return
		}
	}
	if len(query["symbol"]) != 1 {
		writeAdminError(writer, http.StatusBadRequest, "history", "invalid_symbol", "History symbol is required.")
		return
	}
	symbol := query.Get("symbol")
	normalized, err := domain.NormalizeSymbol(symbol)
	if err != nil || symbol != normalized {
		writeAdminError(writer, http.StatusBadRequest, "history", "invalid_symbol", "History symbol must be canonical.")
		return
	}
	pageSize, err := parseCanonicalHistoryPageSize(query.Get("page_size"))
	if err != nil {
		writeAdminError(writer, http.StatusBadRequest, "history", "invalid_page_size", "History page_size must be from 1 to 50.")
		return
	}
	token, err := storage.DecodePageKey(query.Get("page_key"))
	if err != nil {
		writeAdminError(writer, http.StatusBadRequest, "history", "invalid_page_key", "History page key is invalid.")
		return
	}
	data, err := s.readHistory(symbol, pageSize, token)
	if err != nil {
		status, code := historyError(err)
		writeAdminError(writer, status, "history", code, historyErrorMessage(code))
		if code == "storage_unavailable" {
			s.signalHistoryFatal(err)
		}
		return
	}
	now := time.Now()
	envelope := SuccessEnvelope[HistoryData]{
		SchemaVersion: 1,
		Command:       "history",
		GeneratedAt:   now.UTC().Format(time.RFC3339Nano),
		Data:          data,
	}
	writeAdminJSON(writer, http.StatusOK, envelope, "history")
}

func parseCanonicalHistoryPageSize(value string) (uint32, error) {
	if value == "" {
		return storage.DefaultHistoryPageSize, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed < storage.MinHistoryPageSize || parsed > storage.MaxHistoryPageSize ||
		strconv.FormatUint(parsed, 10) != value {
		return 0, errors.New("page_size must be a canonical decimal from 1 to 50")
	}
	return uint32(parsed), nil
}

func (s *AdminServer) readHistory(symbol string, pageSize uint32, token []byte) (data HistoryData, err error) {
	defer func() {
		if recover() != nil {
			data = HistoryData{}
			err = errors.New("history storage panicked")
		}
	}()
	page, err := s.store.History(symbol, pageSize, token)
	if err != nil {
		return HistoryData{}, err
	}
	return BuildHistoryData(page, s.store.Catalog()), nil
}

func (s *AdminServer) signalHistoryFatal(err error) {
	s.fatalOnce.Do(func() {
		if s.fatal != nil {
			s.fatal(fmt.Errorf("history storage failed after readiness: %w", err))
		}
	})
}

func (s *AdminServer) signalAdminFatal() {
	s.fatalOnce.Do(func() {
		if s.fatal != nil {
			s.fatal(errors.New("admin handler panicked after readiness"))
		}
	})
}

func BuildHistoryData(page storage.HistoryPage, catalog storage.Catalog) HistoryData {
	data := HistoryData{
		Symbol:            page.Symbol,
		HighWaterSequence: uintString(page.HighWater),
		Records:           make([]HistoryRecord, 0, len(page.Records)),
	}
	for _, record := range page.Records {
		provenance := "prior"
		if record.ActivationGeneration == catalog.ActivationGeneration {
			for _, feed := range catalog.Feeds {
				if feed.Symbol == record.Symbol && feed.Fingerprint == record.FeedPlanFingerprint {
					provenance = "current"
					break
				}
			}
		}
		data.Records = append(data.Records, HistoryRecord{
			Value:                 record.Value,
			Sequence:              uintString(record.Sequence),
			ActivationGeneration:  uintString(record.ActivationGeneration),
			CycleStartedAt:        record.CycleStartedAt.UTC().Format(time.RFC3339Nano),
			CollectedAt:           record.CollectedAt.UTC().Format(time.RFC3339Nano),
			ConfiguredSourceCount: record.ConfiguredSources,
			SuccessfulSourceCount: record.SuccessfulSources,
			ContributorIDs:        append([]string{}, record.ContributorIDs...),
			FeedPlanFingerprint:   hex.EncodeToString(record.FeedPlanFingerprint[:]),
			Provenance:            provenance,
		})
	}
	if len(page.NextPageToken) > 0 {
		encoded := storage.EncodePageKey(page.NextPageToken)
		data.NextPageKey = &encoded
	}
	return data
}

func writeAdminJSON(writer http.ResponseWriter, status int, value any, command string) {
	encoded, err := json.Marshal(value)
	if err != nil {
		writeAdminError(writer, http.StatusInternalServerError, command, "internal", "Response serialization failed.")
		return
	}
	if len(encoded) > adminResponseLimit {
		writeAdminError(writer, http.StatusInternalServerError, command, "response_too_large", "Response exceeds the configured limit.")
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = writer.Write(encoded)
}

func writeAdminError(writer http.ResponseWriter, status int, command, code, message string) {
	if len(message) > 512 {
		message = message[:512]
	}
	envelope := ErrorEnvelope{
		SchemaVersion: 1,
		Command:       command,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Error:         ErrorValue{Code: code, Message: message},
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		http.Error(writer, "internal error", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = writer.Write(encoded)
}

func historyError(err error) (int, string) {
	switch {
	case errors.Is(err, storage.ErrHistoryNotFound):
		return http.StatusNotFound, "history_not_found"
	case errors.Is(err, storage.ErrPageKeyMismatch):
		return http.StatusBadRequest, "page_key_symbol_mismatch"
	case errors.Is(err, storage.ErrInvalidPageKey):
		return http.StatusBadRequest, "invalid_page_key"
	case errors.Is(err, storage.ErrPageKeyExpired):
		return http.StatusConflict, "page_key_expired"
	default:
		return http.StatusConflict, "storage_unavailable"
	}
}

func historyErrorMessage(code string) string {
	switch code {
	case "history_not_found":
		return "No configured or stored history exists for the symbol."
	case "page_key_symbol_mismatch":
		return "Page key belongs to another symbol."
	case "invalid_page_key":
		return "Page key is invalid."
	case "page_key_expired":
		return "Page key expired because storage changed or the daemon restarted."
	default:
		return "History storage is unavailable."
	}
}

func commandForPath(path string) string {
	switch path {
	case "/v1/status":
		return "status"
	case "/v1/history":
		return "history"
	default:
		return "admin"
	}
}

func uintString(value uint64) string { return strconv.FormatUint(value, 10) }

func durationString(value time.Duration) string { return strconv.FormatInt(int64(value), 10) }

func ValidateAdminContentType(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "application/json; charset=utf-8")
}
