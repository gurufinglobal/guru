package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/service"
)

const humanTextLimit = 768

type operatorError struct {
	machine string
	reason  string
	hint    string
}

func (e *operatorError) Error() string { return e.machine }

func newOperatorError(machine, reason, hint string) error {
	return &operatorError{machine: machine, reason: reason, hint: hint}
}

type humanDocument struct {
	builder strings.Builder
}

func (d *humanDocument) line(value string) {
	d.builder.WriteString(value)
	d.builder.WriteByte('\n')
}

func (d *humanDocument) blank() {
	d.builder.WriteByte('\n')
}

func (d *humanDocument) labels(title string, rows [][2]string) {
	d.line(title)
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	for _, row := range rows {
		fmt.Fprintf(&d.builder, "  %-*s  %s\n", width, row[0], row[1])
	}
}

func (d *humanDocument) table(title string, header []string, rows [][]string) error {
	d.line(title)
	var table strings.Builder
	writer := tabwriter.NewWriter(&table, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "  "+strings.Join(header, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, "  "+strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	d.builder.WriteString(table.String())
	return nil
}

func (d *humanDocument) writeTo(output io.Writer) error {
	_, err := io.WriteString(output, d.builder.String())
	return err
}

func printableASCII(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return strconv.QuoteToASCII(value)
		}
	}
	return value
}

func boundedHumanText(value string) string {
	value = printableASCII(strings.Join(strings.Fields(value), " "))
	if len(value) <= humanTextLimit {
		return value
	}
	return value[:humanTextLimit-3] + "..."
}

func shellQuoteASCII(value string) string {
	if value != "" {
		safe := true
		for i := 0; i < len(value); i++ {
			character := value[i]
			if (character >= 'a' && character <= 'z') ||
				(character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') ||
				strings.ContainsRune("_@%+=:,./-", rune(character)) {
				continue
			}
			safe = false
			break
		}
		if safe {
			return value
		}
	}
	quoted := strconv.QuoteToASCII(value)
	body := quoted[1 : len(quoted)-1]
	body = strings.ReplaceAll(body, "\\\"", "\"")
	body = strings.ReplaceAll(body, "'", "\\'")
	return "$'" + body + "'"
}

func commandLine(home string, arguments ...string) string {
	parts := []string{"oracled"}
	if home != "" {
		parts = append(parts, "--home", shellQuoteASCII(home))
	}
	for _, argument := range arguments {
		parts = append(parts, shellQuoteASCII(argument))
	}
	return strings.Join(parts, " ")
}

func formatDecimal(value string) (string, error) {
	if _, err := domain.ParseCanonicalDecimal(value); err != nil {
		return "", errors.New("decimal is not canonical")
	}
	dot := strings.IndexByte(value, '.')
	if dot == -1 {
		return value, nil
	}
	fraction := strings.TrimRight(value[dot+1:], "0")
	if fraction == "" {
		return value[:dot], nil
	}
	return value[:dot+1] + fraction, nil
}

func parseHumanTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("timestamp is invalid")
	}
	return parsed.UTC(), nil
}

func formatAge(observed, now time.Time) string {
	if observed.After(now) {
		return "clock anomaly"
	}
	seconds := int64(now.Sub(observed) / time.Second)
	switch {
	case seconds == 0:
		return "<1s"
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 60*60:
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	case seconds < 24*60*60:
		return fmt.Sprintf("%dh %dm", seconds/(60*60), (seconds%(60*60))/60)
	default:
		return fmt.Sprintf("%dd %dh", seconds/(24*60*60), (seconds%(24*60*60))/(60*60))
	}
}

func formatTimeAndAge(value string, now time.Time) (string, string, error) {
	parsed, err := parseHumanTime(value)
	if err != nil {
		return "", "", err
	}
	return parsed.Truncate(time.Second).Format(time.RFC3339), formatAge(parsed, now), nil
}

func formatDurationNanos(value string) (string, error) {
	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil || nanos < 0 {
		return "", errors.New("duration is invalid")
	}
	return time.Duration(nanos).String(), nil
}

func sourceRatio(successful, configured uint32, known bool) string {
	if !known {
		return fmt.Sprintf("-/%d", configured)
	}
	return fmt.Sprintf("%d/%d", successful, configured)
}

func humanHealth(value string) string {
	switch value {
	case "healthy":
		return "healthy"
	case "degraded":
		return "degraded"
	case "unavailable":
		return "unavailable"
	default:
		return "unknown"
	}
}

func humanFreshness(value string) string {
	switch value {
	case "fresh":
		return "fresh"
	case "stale":
		return "stale"
	case "no_value":
		return "no value"
	case "clock_anomaly":
		return "clock anomaly"
	default:
		return "unknown"
	}
}

func humanActivity(value string) string {
	switch value {
	case "idle":
		return "idle"
	case "in_flight":
		return "collecting"
	default:
		return "unknown"
	}
}

func humanOutcome(value string) string {
	switch value {
	case "never":
		return "not run"
	case "full":
		return "all sources"
	case "quorum":
		return "majority"
	case "under_quorum":
		return "insufficient sources"
	case "cancelled":
		return "cancelled"
	default:
		return "unknown"
	}
}

func humanOmission(value *string) string {
	if value == nil {
		return "-"
	}
	switch *value {
	case "unconfigured":
		return "not configured"
	case "no_value":
		return "no aggregate"
	case "stale":
		return "aggregate is stale"
	case "clock_anomaly":
		return "clock anomaly"
	case "prior_generation":
		return "aggregate is from prior configuration"
	case "under_quorum":
		return "insufficient sources"
	default:
		return "unavailable"
	}
}

func printHumanError(output io.Writer, command, code, home string, err error) error {
	reason, hint := humanErrorDetails(command, code, home, err)
	var document humanDocument
	document.line("oracled could not complete the command.")
	document.blank()
	document.labels("Reason", [][2]string{{"", reason}})
	if hint != "" {
		document.blank()
		document.labels("Hint", [][2]string{{"", hint}})
	}
	return document.writeTo(output)
}

func humanErrorDetails(command, code, home string, err error) (string, string) {
	var operatorFailure *operatorError
	if errors.As(err, &operatorFailure) {
		return boundedHumanText(operatorFailure.reason), boundedHumanText(operatorFailure.hint)
	}
	helpArguments := []string{"help"}
	if command != "" {
		helpArguments = []string{command, "--help"}
	}
	switch code {
	case "invalid_arguments":
		return boundedHumanText(err.Error()), "Run " + commandLine(home, helpArguments...) + " for valid usage."
	case "invalid_config":
		return "The sidecar home is missing or its published configuration is invalid.",
			"Run " + commandLine(home, "validate") + ". If the home is new, run " + commandLine(home, "init") + "."
	case "home_locked":
		return "The sidecar home is temporarily owned by another process.",
			"The daemon may be running, starting, or stopping. Wait for it to settle, then retry."
	case "daemon_unavailable":
		return "The running sidecar could not be reached.",
			"Start it with " + commandLine(home, "start") + ", then retry."
	case "protocol_error":
		if command == "reconcile" && isReconcileProtocolError(err) {
			return "The Guru node returned an incompatible or invalid gRPC response.",
				"Verify that gurud and oracled use compatible builds, then retry."
		}
		return "The sidecar returned an incompatible or invalid admin response.",
			"Verify that the CLI and daemon are the same build, then restart the daemon."
	case "storage_error":
		return "The sidecar could not read its local storage safely.",
			"Check the home permissions and disk, then retry after the daemon is stopped."
	case "node_unavailable":
		return "The Guru node gRPC endpoint could not be reached.",
			"Verify that gurud gRPC is listening, or override it with --node-grpc."
	default:
		return "The command failed because of an internal error.",
			"Retry the command. If it fails again, inspect the daemon logs."
	}
}

func printInitialized(output io.Writer, pair *config.Pair) error {
	var document humanDocument
	document.line("oracled is initialized.")
	document.blank()
	document.labels("Application", [][2]string{
		{"Home", printableASCII(pair.Paths.Home)},
		{"Feeds", strconv.Itoa(len(pair.Feeds))},
		{"Sources", strconv.Itoa(configuredSourceCount(pair))},
	})
	document.blank()
	document.labels("Next", [][2]string{
		{"Validate", commandLine(pair.Paths.Home, "validate")},
		{"Start", commandLine(pair.Paths.Home, "start")},
	})
	return document.writeTo(output)
}

func printValidated(output io.Writer, pair *config.Pair) error {
	var document humanDocument
	document.line("Configuration is valid.")
	document.blank()
	document.labels("Configuration", [][2]string{
		{"Home", printableASCII(pair.Paths.Home)},
		{"Revision", printableASCII(pair.Config.PublicationRevision)},
		{"Feeds", strconv.Itoa(len(pair.Feeds))},
		{"Sources", strconv.Itoa(configuredSourceCount(pair))},
	})
	document.blank()
	document.labels("Next", [][2]string{
		{"Start", commandLine(pair.Paths.Home, "start")},
	})
	return document.writeTo(output)
}

func configuredSourceCount(pair *config.Pair) int {
	total := 0
	for _, feed := range pair.Feeds {
		total += len(feed.Sources)
	}
	return total
}

func printLiveStatus(
	output io.Writer,
	home string,
	data service.StatusData,
	selected *service.FeedStatus,
	now time.Time,
) error {
	if selected != nil {
		return printLiveFeedStatus(output, home, data, *selected, now)
	}
	var document humanDocument
	switch data.Health {
	case "healthy":
		document.line("Oracle daemon is healthy.")
	case "degraded":
		document.line("Oracle daemon is degraded.")
	default:
		document.line("Oracle daemon has no available feeds.")
	}
	document.blank()
	document.labels("Daemon", [][2]string{
		{"State", "running"},
		{"Home", printableASCII(home)},
		{"Health", humanHealth(data.Health)},
		{"Generation", printableASCII(data.ActivationGeneration)},
		{"Feeds", strconv.Itoa(len(data.Feeds))},
	})
	document.blank()
	rows := make([][]string, 0, len(data.Feeds))
	for _, feed := range data.Feeds {
		sources := sourceRatio(feed.Cycle.SuccessfulSourceCount, feed.ConfiguredSourceCount, feed.Cycle.CompletedAt != nil)
		collected, age := "-", "-"
		if feed.Latest != nil {
			sources = sourceRatio(feed.Latest.SuccessfulSourceCount, feed.ConfiguredSourceCount, true)
			var err error
			collected, age, err = formatTimeAndAge(feed.Latest.CollectedAt, now)
			if err != nil {
				return err
			}
		}
		rows = append(rows, []string{
			printableASCII(feed.Symbol),
			humanHealth(feed.Health),
			humanFreshness(feed.Freshness),
			sources,
			humanActivity(feed.Cycle.Activity),
			humanOutcome(feed.Cycle.LastOutcome),
			collected,
			age,
		})
	}
	if err := document.table(
		"Feeds",
		[]string{"SYMBOL", "HEALTH", "FRESHNESS", "SOURCES", "ACTIVITY", "LAST RESULT", "COLLECTED", "AGE"},
		rows,
	); err != nil {
		return err
	}
	if len(data.Feeds) > 0 {
		document.blank()
		document.line("Details: " + commandLine(home, "status", data.Feeds[0].Symbol))
	}
	return document.writeTo(output)
}

func printLiveFeedStatus(
	output io.Writer,
	home string,
	data service.StatusData,
	feed service.FeedStatus,
	now time.Time,
) error {
	sources := sourceRatio(feed.Cycle.SuccessfulSourceCount, feed.ConfiguredSourceCount, feed.Cycle.CompletedAt != nil)
	collected, age := "-", "-"
	if feed.Latest != nil {
		sources = sourceRatio(feed.Latest.SuccessfulSourceCount, feed.ConfiguredSourceCount, true)
		var err error
		collected, age, err = formatTimeAndAge(feed.Latest.CollectedAt, now)
		if err != nil {
			return err
		}
	}
	interval, err := formatDurationNanos(feed.IntervalNanos)
	if err != nil {
		return err
	}
	staleAfter, err := formatDurationNanos(feed.StaleAfterNanos)
	if err != nil {
		return err
	}
	var document humanDocument
	document.line("Feed " + printableASCII(feed.Symbol) + " is " + humanHealth(feed.Health) + ".")
	document.blank()
	document.labels("Daemon", [][2]string{
		{"State", "running"},
		{"Home", printableASCII(home)},
		{"Health", humanHealth(data.Health)},
		{"Generation", printableASCII(data.ActivationGeneration)},
	})
	document.blank()
	document.labels("Feed", [][2]string{
		{"Symbol", printableASCII(feed.Symbol)},
		{"Health", humanHealth(feed.Health)},
		{"Freshness", humanFreshness(feed.Freshness)},
		{"Sources", sources},
		{"Activity", humanActivity(feed.Cycle.Activity)},
		{"Last result", humanOutcome(feed.Cycle.LastOutcome)},
		{"Collected", collected},
		{"Age", age},
		{"Interval", interval},
		{"Stale after", staleAfter},
		{"Omission", humanOmission(feed.OmissionReason)},
	})
	document.blank()
	document.line("History: " + commandLine(home, "history", feed.Symbol))
	return document.writeTo(output)
}

func printOfflineStatus(
	output io.Writer,
	view offlineStatusView,
	selected *offlineStatusFeed,
	now time.Time,
) error {
	if selected != nil {
		return printOfflineFeedStatus(output, view, *selected, now)
	}
	var document humanDocument
	document.line("Oracle daemon is stopped.")
	document.blank()
	document.labels("Daemon", [][2]string{
		{"State", "stopped"},
		{"Home", printableASCII(view.Home)},
		{"Configuration", view.Configuration},
		{"Generation", view.Generation},
		{"Feeds", strconv.Itoa(len(view.Feeds))},
	})
	document.blank()
	rows := make([][]string, 0, len(view.Feeds))
	for _, feed := range view.Feeds {
		collected, age := "-", "-"
		if feed.CollectedAt != "" {
			var err error
			collected, age, err = formatTimeAndAge(feed.CollectedAt, now)
			if err != nil {
				return err
			}
		}
		rows = append(rows, []string{
			printableASCII(feed.Symbol),
			feed.RecordState,
			sourceRatio(feed.SuccessfulSources, feed.ConfiguredSources, feed.HasRecord),
			collected,
			age,
			feed.Provenance,
		})
	}
	if err := document.table(
		"Stored feeds",
		[]string{"SYMBOL", "RECORD", "SOURCES", "COLLECTED", "AGE", "PROVENANCE"},
		rows,
	); err != nil {
		return err
	}
	document.blank()
	document.line("Start live collection: " + commandLine(view.Home, "start"))
	return document.writeTo(output)
}

func printOfflineFeedStatus(
	output io.Writer,
	view offlineStatusView,
	feed offlineStatusFeed,
	now time.Time,
) error {
	collected, age := "-", "-"
	if feed.CollectedAt != "" {
		var err error
		collected, age, err = formatTimeAndAge(feed.CollectedAt, now)
		if err != nil {
			return err
		}
	}
	var document humanDocument
	document.line("Oracle daemon is stopped.")
	document.blank()
	document.labels("Daemon", [][2]string{
		{"State", "stopped"},
		{"Home", printableASCII(view.Home)},
		{"Configuration", view.Configuration},
		{"Generation", view.Generation},
	})
	document.blank()
	document.labels("Stored feed", [][2]string{
		{"Symbol", printableASCII(feed.Symbol)},
		{"Record", feed.RecordState},
		{"Sources", sourceRatio(feed.SuccessfulSources, feed.ConfiguredSources, feed.HasRecord)},
		{"Collected", collected},
		{"Age", age},
		{"Provenance", feed.Provenance},
	})
	document.blank()
	document.line("Start live collection: " + commandLine(view.Home, "start"))
	return document.writeTo(output)
}

func printHistorySummary(output io.Writer, view historySummaryView, now time.Time) error {
	var document humanDocument
	document.line("Stored aggregate summary.")
	document.blank()
	document.labels("History", [][2]string{
		{"Mode", view.Mode},
		{"Home", printableASCII(view.Home)},
		{"Feeds", strconv.Itoa(len(view.Rows))},
	})
	document.blank()
	rows := make([][]string, 0, len(view.Rows))
	for _, record := range view.Rows {
		value, collected, age, provenance := "no records", "-", "-", "-"
		sources := sourceRatio(0, record.ConfiguredSources, false)
		if record.HasRecord {
			var err error
			value, err = formatDecimal(record.Value)
			if err != nil {
				return err
			}
			collected, age, err = formatTimeAndAge(record.CollectedAt, now)
			if err != nil {
				return err
			}
			provenance = printableASCII(record.Provenance)
			sources = sourceRatio(record.SuccessfulSources, record.ConfiguredSources, true)
		}
		rows = append(rows, []string{
			printableASCII(record.Symbol),
			value,
			sources,
			collected,
			age,
			provenance,
		})
	}
	if err := document.table(
		"Feeds",
		[]string{"SYMBOL", "LATEST VALUE", "SOURCES", "COLLECTED", "AGE", "PROVENANCE"},
		rows,
	); err != nil {
		return err
	}
	if len(view.Rows) > 0 {
		document.blank()
		document.line("Details: " + commandLine(view.Home, "history", view.Rows[0].Symbol))
	}
	if view.Mode == "offline" {
		document.blank()
		document.line("Start live collection: " + commandLine(view.Home, "start"))
	}
	return document.writeTo(output)
}

func printHistoryDetail(
	output io.Writer,
	home, mode string,
	data service.HistoryData,
	pageSize uint32,
	offline bool,
	now time.Time,
) error {
	var document humanDocument
	document.line("History for " + printableASCII(data.Symbol) + ".")
	document.blank()
	document.labels("History", [][2]string{
		{"Mode", mode},
		{"Home", printableASCII(home)},
		{"Symbol", printableASCII(data.Symbol)},
		{"Records", strconv.Itoa(len(data.Records))},
	})
	document.blank()
	if len(data.Records) == 0 {
		document.line("No retained records.")
	} else {
		rows := make([][]string, 0, len(data.Records))
		for _, record := range data.Records {
			value, err := formatDecimal(record.Value)
			if err != nil {
				return err
			}
			collected, age, err := formatTimeAndAge(record.CollectedAt, now)
			if err != nil {
				return err
			}
			rows = append(rows, []string{
				printableASCII(record.Sequence),
				value,
				sourceRatio(record.SuccessfulSourceCount, record.ConfiguredSourceCount, true),
				collected,
				age,
				printableASCII(record.Provenance),
			})
		}
		if err := document.table(
			"Records",
			[]string{"SEQ", "VALUE", "SOURCES", "COLLECTED", "AGE", "PROVENANCE"},
			rows,
		); err != nil {
			return err
		}
	}
	if data.NextPageKey != nil {
		arguments := []string{
			"history",
			data.Symbol,
			"--page-size",
			strconv.FormatUint(uint64(pageSize), 10),
			"--page-key",
			*data.NextPageKey,
		}
		if offline {
			arguments = append(arguments, "--offline")
		}
		document.blank()
		document.labels("Next page", [][2]string{
			{"Command", commandLine(home, arguments...)},
		})
	}
	if offline {
		document.blank()
		document.line("Start live collection: " + commandLine(home, "start"))
	}
	return document.writeTo(output)
}

type findingText struct {
	issue  string
	action string
}

func findingPresentation(code string) findingText {
	switch code {
	case "runtime_config_mismatch":
		return findingText{
			issue:  "running configuration differs from disk",
			action: "restart the sidecar",
		}
	case "unsupported_task_type":
		return findingText{
			issue:  "active task is not numeric",
			action: "review the on-chain task",
		}
	case "missing_symbol":
		return findingText{
			issue:  "active task is not configured locally",
			action: "add suitable local sources and restart",
		}
	case "configured_sources_below_minimum":
		return findingText{
			issue:  "configured sources are below the required minimum",
			action: "add independent local sources",
		}
	case "no_value":
		return findingText{
			issue:  "no current aggregate is available",
			action: "wait for a successful collection",
		}
	case "stale":
		return findingText{
			issue:  "latest aggregate is stale",
			action: "check source availability",
		}
	case "clock_anomaly":
		return findingText{
			issue:  "latest aggregate has a clock anomaly",
			action: "check the host clock",
		}
	case "aggregate_sources_below_minimum":
		return findingText{
			issue:  "latest aggregate used too few sources",
			action: "restore enough source responses",
		}
	case "under_quorum":
		return findingText{
			issue:  "latest cycle was below local quorum",
			action: "monitor source recovery",
		}
	case "inactive_symbol":
		return findingText{
			issue:  "local symbol is not an active task",
			action: "no action required",
		}
	default:
		return findingText{
			issue:  "unrecognized readiness finding",
			action: "inspect matching-version logs",
		}
	}
}

func printReconcile(output io.Writer, status service.StatusData, data ReconcileData) error {
	blocking := false
	for _, finding := range data.Findings {
		if finding.Blocking {
			blocking = true
			break
		}
	}
	var document humanDocument
	if blocking {
		document.line("Action required.")
	} else {
		document.line("Ready to contribute.")
	}
	document.blank()
	document.labels("Readiness", [][2]string{
		{"Node", printableASCII(data.NodeGRPC)},
		{"Sidecar health", humanHealth(status.Health)},
		{"Configured feeds", strconv.Itoa(len(status.Feeds))},
		{"Active tasks", strconv.FormatUint(uint64(data.ActiveTaskCount), 10)},
		{"Minimum sources", strconv.FormatUint(uint64(data.MinSources), 10)},
	})
	document.blank()
	if len(data.Findings) == 0 {
		document.line("No readiness mismatches found.")
		return document.writeTo(output)
	}
	rows := make([][]string, 0, len(data.Findings))
	for _, finding := range data.Findings {
		severity := "info"
		if finding.Blocking {
			severity = "blocking"
		}
		symbol := "-"
		if finding.Symbol != nil {
			symbol = printableASCII(*finding.Symbol)
		}
		presentation := findingPresentation(finding.Code)
		rows = append(rows, []string{
			severity,
			symbol,
			presentation.issue,
			presentation.action,
		})
	}
	if err := document.table(
		"Findings",
		[]string{"SEVERITY", "SYMBOL", "ISSUE", "ACTION"},
		rows,
	); err != nil {
		return err
	}
	return document.writeTo(output)
}
