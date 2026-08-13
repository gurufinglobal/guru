package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/service"
)

func TestHumanDecimalFormattingIsExact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{input: "0.000000000000000000", want: "0"},
		{input: "10.000000000000000000", want: "10"},
		{input: "63699.135000000000000000", want: "63699.135"},
		{input: "0.000000000000000001", want: "0.000000000000000001"},
		{input: "-73.618500000000000000", want: "-73.6185"},
		{input: "999999999999999999999999999999.123456789012345678", want: "999999999999999999999999999999.123456789012345678"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := formatDecimal(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("formatDecimal(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
	for _, invalid := range []string{"", "0", "001.000000000000000000", "-", ".1", "1.", "1e3", "1.2.3", "+1"} {
		if _, err := formatDecimal(invalid); err == nil {
			t.Fatalf("formatDecimal(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestHumanTimeAgeAndASCIIFormatting(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_090_061, 500_000_000).UTC()
	tests := []struct {
		observed time.Time
		want     string
	}{
		{observed: now, want: "<1s"},
		{observed: now.Add(-59 * time.Second), want: "59s"},
		{observed: now.Add(-61 * time.Second), want: "1m 1s"},
		{observed: now.Add(-61 * time.Minute), want: "1h 1m"},
		{observed: now.Add(-25 * time.Hour), want: "1d 1h"},
		{observed: time.Unix(now.Unix()-1, 999_000_000), want: "<1s"},
		{observed: now.Add(time.Millisecond), want: "clock anomaly"},
		{observed: now.Add(time.Second), want: "clock anomaly"},
	}
	for _, test := range tests {
		if got := formatAge(test.observed, now); got != test.want {
			t.Fatalf("formatAge(%s) = %q, want %q", test.observed, got, test.want)
		}
	}
	if got := printableASCII("BTC/\t한"); got != "\"BTC/\\t\\ud55c\"" {
		t.Fatalf("printableASCII = %q", got)
	}
	if got := shellQuoteASCII("/tmp/a b"); got != "$'/tmp/a b'" {
		t.Fatalf("shellQuoteASCII = %q", got)
	}
}

func TestHumanHistoryDetailHasHeadersAndReadableValues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 3, 0, 10, 0, time.UTC)
	data := service.HistoryData{
		Symbol:            "BTC/USD",
		HighWaterSequence: "9",
		Records: []service.HistoryRecord{{
			Sequence:              "9",
			Value:                 "63699.135000000000000000",
			ConfiguredSourceCount: 4,
			SuccessfulSourceCount: 4,
			CollectedAt:           now.Add(-10 * time.Second).Format(time.RFC3339Nano),
			Provenance:            "current",
		}},
	}
	var output bytes.Buffer
	if err := printHistoryDetail(&output, "/private/tmp/oracle", "live", data, 30, false, now); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		"History for BTC/USD.",
		"Mode     live",
		"SEQ",
		"VALUE",
		"63699.135",
		"4/4",
		"2026-08-04T03:00:00Z",
		"10s",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("history output missing %q:\n%s", expected, text)
		}
	}
	for _, forbidden := range []string{"63699.135000000000000000", "high water", "\t", "\x1b"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("history output contains %q:\n%s", forbidden, text)
		}
	}
}

func TestHumanStatusNeverPrintsAggregateValues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 3, 0, 10, 0, time.UTC)
	data := service.StatusData{
		Health:               "healthy",
		ActivationGeneration: "1",
		Feeds: []service.FeedStatus{{
			Symbol:                "BTC/USD",
			ConfiguredSourceCount: 4,
			IntervalNanos:         "10000000000",
			StaleAfterNanos:       "30000000000",
			Freshness:             "fresh",
			Health:                "healthy",
			Latest: &service.LatestStatus{
				Sequence:              "1",
				CollectedAt:           now.Add(-time.Second).Format(time.RFC3339Nano),
				SuccessfulSourceCount: 4,
			},
			Cycle: service.CycleStatus{
				Activity:              "idle",
				LastOutcome:           "full",
				SuccessfulSourceCount: 4,
			},
		}},
	}
	var output bytes.Buffer
	if err := printLiveStatus(&output, "/private/tmp/oracle", data, nil, now); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{"Oracle daemon is healthy.", "SYMBOL", "FRESHNESS", "4/4", "all sources"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("status output missing %q:\n%s", expected, text)
		}
	}
	for _, forbidden := range []string{"63699.135", "value", "https://", "feed_plan_fingerprint", "\t", "\x1b"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("status output contains %q:\n%s", forbidden, text)
		}
	}
}

func TestHumanErrorsHideImplementationDetails(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := errors.New("Get http://unix/v1/status: dial unix /secret/admin.sock")
	if writeErr := printHumanError(&output, "status", "daemon_unavailable", "/private/tmp/oracle", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	text := output.String()
	for _, expected := range []string{"oracled could not complete the command.", "Reason", "Hint", "oracled --home /private/tmp/oracle start"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("human error missing %q:\n%s", expected, text)
		}
	}
	for _, forbidden := range []string{"http://unix", "/secret/admin.sock", "\t", "\x1b"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("human error contains %q:\n%s", forbidden, text)
		}
	}
}

func TestHumanReconcileProtocolErrorIdentifiesTheNode(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := asReconcileProtocolError(errors.New("node returned malformed pagination"))
	if writeErr := printHumanError(&output, "reconcile", "protocol_error", "/private/tmp/oracle", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	text := output.String()
	if !strings.Contains(text, "Guru node returned an incompatible") ||
		!strings.Contains(text, "compatible builds") ||
		strings.Contains(text, "restart the daemon") {
		t.Fatalf("reconcile protocol error is misleading:\n%s", text)
	}
}

func TestHumanRendererPropagatesWriterFailure(t *testing.T) {
	t.Parallel()
	view := historySummaryView{
		Mode: "offline",
		Home: "/private/tmp/oracle",
		Rows: []historySummaryRow{{
			Symbol:            "BTC/USD",
			ConfiguredSources: 4,
		}},
	}
	if err := printHistorySummary(failingHumanWriter{}, view, time.Now()); err == nil {
		t.Fatal("renderer ignored writer failure")
	}
}

func TestHumanReconcileVerdictsHideInternalCodes(t *testing.T) {
	t.Parallel()
	symbol := "SOL/USD"
	tests := []struct {
		name      string
		findings  []Finding
		firstLine string
		contains  string
	}{
		{name: "ready", firstLine: "Ready to contribute.", contains: "No readiness mismatches found."},
		{
			name: "blocking",
			findings: []Finding{{
				Code:     "missing_symbol",
				Blocking: true,
				Symbol:   &symbol,
			}},
			firstLine: "Action required.",
			contains:  "active task is not configured locally",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := printReconcile(
				&output,
				service.StatusData{Health: "healthy", Feeds: []service.FeedStatus{{Symbol: "BTC/USD"}}},
				ReconcileData{
					NodeGRPC:        "127.0.0.1:9090",
					MinSources:      3,
					ActiveTaskCount: 3,
					Findings:        test.findings,
				},
			); err != nil {
				t.Fatal(err)
			}
			text := output.String()
			if !strings.HasPrefix(text, test.firstLine+"\n") || !strings.Contains(text, test.contains) {
				t.Fatalf("reconcile output:\n%s", text)
			}
			if strings.Contains(text, "missing_symbol") || strings.Contains(text, "\t") {
				t.Fatalf("reconcile output exposes internal representation:\n%s", text)
			}
		})
	}
}

type failingHumanWriter struct{}

func (failingHumanWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}
