package cli

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/service"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

type historySummaryView struct {
	Mode string
	Home string
	Rows []historySummaryRow
}

type historySummaryRow struct {
	Symbol            string
	Value             string
	CollectedAt       string
	Provenance        string
	ConfiguredSources uint32
	SuccessfulSources uint32
	HasRecord         bool
}

type offlineStatusView struct {
	Home          string
	Configuration string
	Generation    string
	Feeds         []offlineStatusFeed
}

type offlineStatusFeed struct {
	Symbol            string
	CollectedAt       string
	RecordState       string
	Provenance        string
	ConfiguredSources uint32
	SuccessfulSources uint32
	HasRecord         bool
}

type offlineAccessError struct {
	code string
	err  error
}

func (e *offlineAccessError) Error() string { return e.err.Error() }
func (e *offlineAccessError) Unwrap() error { return e.err }

func configuredSymbols(feeds []domain.FeedPlan) []string {
	symbols := make([]string, len(feeds))
	for i, feed := range feeds {
		symbols[i] = feed.Symbol
	}
	sort.Strings(symbols)
	return symbols
}

func resolveConfiguredSymbol(raw string, feeds []domain.FeedPlan) (string, error) {
	symbols := configuredSymbols(feeds)
	if len(symbols) == 0 {
		return "", newOperatorError(
			"no symbols are configured",
			"No symbols are configured.",
			"Add a feed to sources.toml, run validate, and restart the sidecar.",
		)
	}
	for _, symbol := range symbols {
		if raw == symbol {
			return symbol, nil
		}
	}
	trimmed := strings.TrimSpace(raw)
	caseMatches := matchingSymbols(symbols, func(symbol string) bool {
		return strings.EqualFold(trimmed, symbol)
	})
	if len(caseMatches) == 1 {
		return caseMatches[0], nil
	}
	if len(caseMatches) > 1 {
		return "", ambiguousSymbolError(raw, symbols)
	}
	alias := separatorKey(trimmed)
	aliasMatches := matchingSymbols(symbols, func(symbol string) bool {
		return strings.EqualFold(alias, separatorKey(symbol))
	})
	if len(aliasMatches) == 1 {
		return aliasMatches[0], nil
	}
	if len(aliasMatches) > 1 {
		return "", ambiguousSymbolError(raw, symbols)
	}
	available := displaySymbolList(symbols)
	example := "BTC/USD"
	if len(symbols) > 0 {
		example = symbols[0]
	}
	return "", newOperatorError(
		"symbol is not configured",
		"Unknown symbol "+printableASCII(raw)+". Available symbols: "+available+".",
		"Use a configured symbol, for example "+printableASCII(example)+".",
	)
}

func matchingSymbols(symbols []string, match func(string) bool) []string {
	var matches []string
	for _, symbol := range symbols {
		if match(symbol) {
			matches = append(matches, symbol)
		}
	}
	return matches
}

func separatorKey(value string) string {
	return strings.Map(func(character rune) rune {
		switch character {
		case '/', '-', '_':
			return '/'
		default:
			return character
		}
	}, value)
}

func ambiguousSymbolError(raw string, symbols []string) error {
	return newOperatorError(
		"symbol alias is ambiguous",
		"Symbol "+printableASCII(raw)+" matches more than one configured symbol. Available symbols: "+
			displaySymbolList(symbols)+".",
		"Use the exact canonical symbol shown in the configuration.",
	)
}

func displaySymbolList(symbols []string) string {
	display := make([]string, len(symbols))
	for i, symbol := range symbols {
		display[i] = printableASCII(symbol)
	}
	return strings.Join(display, ", ")
}

func fetchHistory(
	ctx context.Context,
	socket, symbol string,
	pageSize uint32,
	pageKey string,
	validateHumanData bool,
) ([]byte, service.HistoryData, error) {
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("page_size", strconv.FormatUint(uint64(pageSize), 10))
	if pageKey != "" {
		query.Set("page_key", pageKey)
	}
	body, statusCode, err := fetchAdmin(ctx, socket, "/v1/history?"+query.Encode())
	if err != nil {
		return nil, service.HistoryData{}, err
	}
	if statusCode != http.StatusOK {
		return nil, service.HistoryData{}, asAdminProtocolError(decodeAdminError(body))
	}
	envelope, err := decodeSuccess[service.HistoryData](body, "history")
	if err != nil {
		return nil, service.HistoryData{}, asAdminProtocolError(err)
	}
	if validateHumanData {
		if envelope.Data.Symbol != symbol || len(envelope.Data.Records) > int(pageSize) {
			return nil, service.HistoryData{}, asAdminProtocolError(errors.New("admin history response does not match the request"))
		}
		if err := validateHumanHistoryData(envelope.Data); err != nil {
			return nil, service.HistoryData{}, asAdminProtocolError(err)
		}
	}
	return body, envelope.Data, nil
}

func liveHistorySummary(
	ctx context.Context,
	pair *config.Pair,
) (historySummaryView, error) {
	view := historySummaryView{
		Mode: "live",
		Home: pair.Paths.Home,
		Rows: make([]historySummaryRow, 0, len(pair.Feeds)),
	}
	if len(pair.Feeds) == 0 {
		body, statusCode, err := fetchAdmin(ctx, pair.Paths.AdminSocket, "/v1/status")
		if err != nil {
			return historySummaryView{}, err
		}
		if statusCode != http.StatusOK {
			return historySummaryView{}, asAdminProtocolError(decodeAdminError(body))
		}
		envelope, err := decodeSuccess[service.StatusData](body, "status")
		if err != nil {
			return historySummaryView{}, asAdminProtocolError(err)
		}
		if err := validateHumanStatusData(envelope.Data); err != nil {
			return historySummaryView{}, asAdminProtocolError(err)
		}
		return view, nil
	}
	for _, feed := range pair.Feeds {
		_, data, err := fetchHistory(
			ctx,
			pair.Paths.AdminSocket,
			feed.Symbol,
			storage.MinHistoryPageSize,
			"",
			true,
		)
		if err != nil {
			return historySummaryView{}, err
		}
		row := historySummaryRow{
			Symbol:            feed.Symbol,
			ConfiguredSources: uint32(len(feed.Sources)),
		}
		if len(data.Records) > 0 {
			record := data.Records[0]
			row.Value = record.Value
			row.CollectedAt = record.CollectedAt
			row.Provenance = record.Provenance
			row.ConfiguredSources = record.ConfiguredSourceCount
			row.SuccessfulSources = record.SuccessfulSourceCount
			row.HasRecord = true
		}
		view.Rows = append(view.Rows, row)
	}
	return view, nil
}

func withOfflineStore(
	home string,
	read func(*config.Pair, *storage.Store) error,
) (resultErr error) {
	lockPath, err := config.CanonicalHomeLockPath(home)
	if err != nil {
		return &offlineAccessError{code: "invalid_config", err: err}
	}
	lock, err := storage.AcquireHomeLock(lockPath)
	if err != nil {
		return &offlineAccessError{code: homeLockFailureCode(err), err: err}
	}
	defer func() {
		if closeErr := lock.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, &offlineAccessError{code: "storage_error", err: closeErr})
		}
	}()
	pair, err := config.Load(home)
	if err != nil {
		return &offlineAccessError{code: "invalid_config", err: err}
	}
	store, err := storage.Open(pair.Paths.Database, pair.Paths.Marker, true)
	if err != nil {
		return &offlineAccessError{code: "storage_error", err: err}
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, &offlineAccessError{code: "storage_error", err: closeErr})
		}
	}()
	if err := read(pair, store); err != nil {
		var accessErr *offlineAccessError
		if errors.As(err, &accessErr) {
			return err
		}
		return &offlineAccessError{code: "storage_error", err: err}
	}
	return nil
}

func commandFailureForOffline(exitCode int, err error) error {
	var accessErr *offlineAccessError
	if errors.As(err, &accessErr) {
		return fail(exitCode, accessErr.code, err)
	}
	return fail(exitCode, "storage_error", err)
}

func offlineStatus(
	home, rawSymbol string,
) (view offlineStatusView, selected *offlineStatusFeed, err error) {
	err = withOfflineStore(home, func(pair *config.Pair, store *storage.Store) error {
		var symbol string
		if rawSymbol != "" {
			resolved, resolveErr := resolveConfiguredSymbol(rawSymbol, pair.Feeds)
			if resolveErr != nil {
				return &offlineAccessError{code: "invalid_arguments", err: resolveErr}
			}
			symbol = resolved
		}
		latest, readErr := store.LatestRecords()
		if readErr != nil {
			return readErr
		}
		catalog := store.Catalog()
		configuration := "pending activation"
		if catalog.PlanDigest == pair.PlanDigest {
			configuration = "activated"
		}
		view = offlineStatusView{
			Home:          pair.Paths.Home,
			Configuration: configuration,
			Generation:    strconv.FormatUint(catalog.ActivationGeneration, 10),
			Feeds:         make([]offlineStatusFeed, 0, len(pair.Feeds)),
		}
		for _, feed := range pair.Feeds {
			record, ok := latest[feed.Symbol]
			item := offlineStatusFeed{
				Symbol:            feed.Symbol,
				RecordState:       "no records",
				Provenance:        "-",
				ConfiguredSources: uint32(len(feed.Sources)),
			}
			if ok {
				item.CollectedAt = record.CollectedAt.UTC().Format(time.RFC3339Nano)
				item.RecordState = "stored"
				item.ConfiguredSources = record.ConfiguredSources
				item.SuccessfulSources = record.SuccessfulSources
				item.HasRecord = true
				item.Provenance = aggregateProvenance(pair, catalog, feed, record)
			}
			view.Feeds = append(view.Feeds, item)
		}
		if symbol != "" {
			for i := range view.Feeds {
				if view.Feeds[i].Symbol == symbol {
					copy := view.Feeds[i]
					selected = &copy
					break
				}
			}
		}
		return nil
	})
	return view, selected, err
}

func aggregateProvenance(
	pair *config.Pair,
	catalog storage.Catalog,
	feed domain.FeedPlan,
	record domain.Aggregate,
) string {
	if catalog.PlanDigest == pair.PlanDigest &&
		record.ActivationGeneration == catalog.ActivationGeneration &&
		record.FeedPlanFingerprint == feed.Fingerprint {
		return "current"
	}
	return "prior"
}

func offlineHistorySummary(home string) (view historySummaryView, err error) {
	err = withOfflineStore(home, func(pair *config.Pair, store *storage.Store) error {
		latest, readErr := store.LatestRecords()
		if readErr != nil {
			return readErr
		}
		catalog := store.Catalog()
		view = historySummaryView{
			Mode: "offline",
			Home: pair.Paths.Home,
			Rows: make([]historySummaryRow, 0, len(pair.Feeds)),
		}
		for _, feed := range pair.Feeds {
			record, ok := latest[feed.Symbol]
			row := historySummaryRow{
				Symbol:            feed.Symbol,
				ConfiguredSources: uint32(len(feed.Sources)),
			}
			if ok {
				row.Value = record.Value
				row.CollectedAt = record.CollectedAt.UTC().Format(time.RFC3339Nano)
				row.Provenance = aggregateProvenance(pair, catalog, feed, record)
				row.ConfiguredSources = record.ConfiguredSources
				row.SuccessfulSources = record.SuccessfulSources
				row.HasRecord = true
			}
			view.Rows = append(view.Rows, row)
		}
		return nil
	})
	return view, err
}

func offlineHistoryDetail(
	home, rawSymbol string,
	pageSize uint32,
	pageKey string,
	allowEmpty bool,
) (data service.HistoryData, resolvedHome string, err error) {
	err = withOfflineStore(home, func(pair *config.Pair, store *storage.Store) error {
		symbol, resolveErr := resolveConfiguredSymbol(rawSymbol, pair.Feeds)
		if resolveErr != nil {
			return &offlineAccessError{code: "invalid_arguments", err: resolveErr}
		}
		token, decodeErr := storage.DecodePageKey(pageKey)
		if decodeErr != nil {
			return &offlineAccessError{code: "invalid_arguments", err: errors.New("--page-key is invalid")}
		}
		page, historyErr := store.History(symbol, pageSize, token)
		if historyErr != nil {
			if allowEmpty && errors.Is(historyErr, storage.ErrHistoryNotFound) {
				data = service.HistoryData{
					Symbol:  symbol,
					Records: []service.HistoryRecord{},
				}
			} else {
				return historyErr
			}
		} else {
			catalog := store.Catalog()
			data = service.BuildHistoryData(page, catalog)
			if allowEmpty && catalog.PlanDigest != pair.PlanDigest {
				for i := range data.Records {
					data.Records[i].Provenance = "prior"
				}
			}
		}
		resolvedHome = pair.Paths.Home
		return nil
	})
	return data, resolvedHome, err
}

func selectLiveFeed(data service.StatusData, symbol string) (*service.FeedStatus, error) {
	if symbol == "" {
		return nil, nil
	}
	for i := range data.Feeds {
		if data.Feeds[i].Symbol == symbol {
			copy := data.Feeds[i]
			return &copy, nil
		}
	}
	return nil, newOperatorError(
		"running sidecar does not contain the configured symbol",
		"The published symbol "+printableASCII(symbol)+" is not present in the running sidecar.",
		"Restart the sidecar to activate the published configuration, then run reconcile.",
	)
}

func validateHumanStatusData(data service.StatusData) error {
	if !oneOf(data.Health, "healthy", "degraded", "unavailable") {
		return errors.New("admin status contains an invalid daemon health")
	}
	if !isCanonicalUint(data.ActivationGeneration) {
		return errors.New("admin status contains an invalid activation generation")
	}
	seen := make(map[string]struct{}, len(data.Feeds))
	for _, feed := range data.Feeds {
		normalized, err := domain.NormalizeSymbol(feed.Symbol)
		if err != nil || normalized != feed.Symbol {
			return errors.New("admin status contains a non-canonical symbol")
		}
		if _, exists := seen[feed.Symbol]; exists {
			return errors.New("admin status contains a duplicate symbol")
		}
		seen[feed.Symbol] = struct{}{}
		if feed.ConfiguredSourceCount == 0 {
			return errors.New("admin status contains a feed without configured sources")
		}
		if _, err := formatDurationNanos(feed.IntervalNanos); err != nil {
			return errors.New("admin status contains an invalid collection interval")
		}
		if _, err := formatDurationNanos(feed.StaleAfterNanos); err != nil {
			return errors.New("admin status contains an invalid stale boundary")
		}
		if !oneOf(feed.Health, "healthy", "degraded", "unavailable") {
			return errors.New("admin status contains an invalid feed health")
		}
		if !oneOf(
			feed.Freshness,
			string(domain.FreshnessNoValue),
			string(domain.FreshnessFresh),
			string(domain.FreshnessStale),
			string(domain.FreshnessClockAnomaly),
		) {
			return errors.New("admin status contains invalid feed freshness")
		}
		hasLatest := feed.Latest != nil
		if (feed.Freshness == string(domain.FreshnessNoValue)) == hasLatest {
			return errors.New("admin status freshness does not match aggregate availability")
		}
		if !oneOf(
			feed.Cycle.Activity,
			string(domain.CycleIdle),
			string(domain.CycleInFlight),
		) {
			return errors.New("admin status contains invalid cycle activity")
		}
		if !oneOf(
			feed.Cycle.LastOutcome,
			string(domain.CycleNever),
			string(domain.CycleFull),
			string(domain.CycleQuorum),
			string(domain.CycleUnderQuorum),
			string(domain.CycleCancelled),
		) {
			return errors.New("admin status contains an invalid cycle outcome")
		}
		if feed.Cycle.SuccessfulSourceCount > feed.ConfiguredSourceCount {
			return errors.New("admin status cycle source count exceeds configured sources")
		}
		if feed.Cycle.CompletedAt != nil {
			if _, err := parseHumanTime(*feed.Cycle.CompletedAt); err != nil {
				return errors.New("admin status contains an invalid cycle timestamp")
			}
		}
		if feed.Latest != nil {
			if feed.Latest.SuccessfulSourceCount > feed.ConfiguredSourceCount {
				return errors.New("admin status aggregate source count exceeds configured sources")
			}
			if _, err := parseHumanTime(feed.Latest.CollectedAt); err != nil {
				return errors.New("admin status contains an invalid aggregate timestamp")
			}
			if !isCanonicalUint(feed.Latest.Sequence) {
				return errors.New("admin status contains an invalid aggregate sequence")
			}
		}
		if feed.OmissionReason != nil && !oneOf(
			*feed.OmissionReason,
			"prior_generation",
			"under_quorum",
			"no_value",
			"stale",
			"clock_anomaly",
		) {
			return errors.New("admin status contains an invalid omission reason")
		}
	}
	return nil
}

func validateHumanHistoryData(data service.HistoryData) error {
	for _, record := range data.Records {
		if _, err := domain.ParseCanonicalDecimal(record.Value); err != nil {
			return errors.New("admin history contains an invalid decimal")
		}
		if _, err := parseHumanTime(record.CollectedAt); err != nil {
			return errors.New("admin history contains an invalid collection timestamp")
		}
		if record.SuccessfulSourceCount > record.ConfiguredSourceCount {
			return errors.New("admin history source count exceeds configured sources")
		}
		if !oneOf(record.Provenance, "current", "prior") {
			return errors.New("admin history contains invalid provenance")
		}
		if !isCanonicalUint(record.Sequence) {
			return errors.New("admin history contains an invalid sequence")
		}
	}
	if data.NextPageKey != nil {
		if *data.NextPageKey == "" {
			return errors.New("admin history contains an invalid next page key")
		}
		if _, err := storage.DecodePageKey(*data.NextPageKey); err != nil {
			return errors.New("admin history contains an invalid next page key")
		}
	}
	return nil
}

func isCanonicalUint(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && strconv.FormatUint(parsed, 10) == value
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func ensureHistoryPageKey(pageKey string) error {
	if _, err := storage.DecodePageKey(pageKey); err != nil {
		return errors.New("--page-key is invalid")
	}
	return nil
}
