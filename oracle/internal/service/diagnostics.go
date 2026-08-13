package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

const (
	diagnosticRepeatInterval = time.Minute
	maxDiagnosticBytes       = 1024
	diagnosticQueueSize      = 256
)

type diagnosticRecord struct {
	Timestamp             string `json:"timestamp"`
	Level                 string `json:"level"`
	Event                 string `json:"event"`
	Symbol                string `json:"symbol,omitempty"`
	Reason                string `json:"reason,omitempty"`
	FeedCount             uint32 `json:"feed_count"`
	ConfiguredSourceCount uint32 `json:"configured_source_count"`
	SuccessfulSourceCount uint32 `json:"successful_source_count"`
}

type Diagnostics struct {
	output  io.Writer
	format  string
	minimum int
	now     func() time.Time
	home    string

	events    chan diagnosticEvent
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
}

type diagnosticEvent struct {
	kind              string
	observedAt        time.Time
	symbol            string
	reason            string
	outcome           domain.CycleOutcome
	feedCount         uint32
	configuredSources uint32
	successfulSources uint32
}

func NewDiagnostics(output io.Writer, logging config.Logging) *Diagnostics {
	return newDiagnostics(output, logging, time.Now)
}

func newDiagnostics(output io.Writer, logging config.Logging, now func() time.Time) *Diagnostics {
	return newDiagnosticsWithHome(output, logging, now, "")
}

func newRuntimeDiagnostics(output io.Writer, logging config.Logging, home string) *Diagnostics {
	return newDiagnosticsWithHome(output, logging, time.Now, home)
}

func newDiagnosticsWithHome(
	output io.Writer,
	logging config.Logging,
	now func() time.Time,
	home string,
) *Diagnostics {
	diagnostics := &Diagnostics{
		output:  output,
		format:  logging.Format,
		minimum: diagnosticLevel(logging.Level),
		now:     now,
		home:    home,
		events:  make(chan diagnosticEvent, diagnosticQueueSize),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go diagnostics.run()
	return diagnostics
}

func (d *Diagnostics) Ready(feedCount uint32) {
	d.enqueue(diagnosticEvent{kind: "ready", feedCount: feedCount})
}

func (d *Diagnostics) CycleFinished(
	symbol string,
	outcome domain.CycleOutcome,
	configuredSources,
	successfulSources uint32,
) {
	d.enqueue(diagnosticEvent{
		kind:              "cycle",
		symbol:            symbol,
		outcome:           outcome,
		configuredSources: configuredSources,
		successfulSources: successfulSources,
	})
}

func (d *Diagnostics) Omitted(symbol, reason string) {
	d.enqueue(diagnosticEvent{kind: "omission", symbol: symbol, reason: reason})
}

func (d *Diagnostics) Close(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		close(d.stop)
	})
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("flush diagnostics: %w", ctx.Err())
	}
}

func (d *Diagnostics) enqueue(event diagnosticEvent) {
	if d == nil || d.closed.Load() {
		return
	}
	event.observedAt = d.now()
	select {
	case d.events <- event:
	default:
	}
}

func (d *Diagnostics) run() {
	defer close(d.done)
	last := make(map[string]time.Time)
	underQuorum := make(map[string]bool)
	for {
		select {
		case event := <-d.events:
			d.handle(event, last, underQuorum)
		case <-d.stop:
			for {
				select {
				case event := <-d.events:
					d.handle(event, last, underQuorum)
				default:
					return
				}
			}
		}
	}
}

func (d *Diagnostics) handle(
	event diagnosticEvent,
	last map[string]time.Time,
	underQuorum map[string]bool,
) {
	switch event.kind {
	case "ready":
		d.emit("info", event.observedAt, diagnosticRecord{Event: "ready", FeedCount: event.feedCount})
	case "cycle":
		wasUnderQuorum := underQuorum[event.symbol]
		if event.outcome == domain.CycleUnderQuorum {
			underQuorum[event.symbol] = true
			key := "cycle_under_quorum\x00" + event.symbol
			if previous, exists := last[key]; !wasUnderQuorum || !exists ||
				event.observedAt.Sub(previous) >= diagnosticRepeatInterval {
				last[key] = event.observedAt
				d.emit("warn", event.observedAt, diagnosticRecord{
					Event:                 "collection_under_quorum",
					Symbol:                event.symbol,
					Reason:                "under_quorum",
					ConfiguredSourceCount: event.configuredSources,
					SuccessfulSourceCount: event.successfulSources,
				})
			}
		} else if wasUnderQuorum &&
			(event.outcome == domain.CycleFull || event.outcome == domain.CycleQuorum) {
			delete(underQuorum, event.symbol)
			d.emit("info", event.observedAt, diagnosticRecord{
				Event:                 "collection_recovered",
				Symbol:                event.symbol,
				ConfiguredSourceCount: event.configuredSources,
				SuccessfulSourceCount: event.successfulSources,
			})
		}
	case "omission":
		key := "consumer_omission\x00" + event.reason
		if previous, exists := last[key]; exists &&
			event.observedAt.Sub(previous) < diagnosticRepeatInterval {
			return
		}
		last[key] = event.observedAt
		d.emit("warn", event.observedAt, diagnosticRecord{
			Event:  "consumer_omission",
			Symbol: event.symbol,
			Reason: event.reason,
		})
	}
}

func (d *Diagnostics) emit(level string, observedAt time.Time, record diagnosticRecord) {
	if d.output == nil || diagnosticLevel(level) < d.minimum {
		return
	}
	record.Timestamp = observedAt.UTC().Format(time.RFC3339Nano)
	record.Level = level

	var line []byte
	if d.format == "json" {
		encoded, err := json.Marshal(record)
		if err != nil {
			return
		}
		line = encoded
	} else {
		line = []byte(formatTextDiagnostic(observedAt, record, d.home))
	}
	if len(line) >= maxDiagnosticBytes {
		line = line[:maxDiagnosticBytes-1]
	}
	line = append(line, '\n')
	_, _ = d.output.Write(line)
}

func formatTextDiagnostic(observedAt time.Time, record diagnosticRecord, home string) string {
	prefix := observedAt.UTC().Truncate(time.Second).Format(time.RFC3339) + " " +
		strings.ToUpper(record.Level)
	switch record.Event {
	case "ready":
		var builder strings.Builder
		fmt.Fprintf(&builder, "%s  Oracle daemon is ready.\n", prefix)
		fmt.Fprintf(&builder, "Home: %s\n", diagnosticASCII(home))
		fmt.Fprintf(&builder, "Feeds: %d\n", record.FeedCount)
		builder.WriteString("Run from another terminal:\n")
		fmt.Fprintf(&builder, "  %s\n", diagnosticCommand(home, "status"))
		fmt.Fprintf(&builder, "  %s\n", diagnosticCommand(home, "history"))
		fmt.Fprintf(&builder, "  %s", diagnosticCommand(home, "reconcile"))
		return builder.String()
	case "collection_under_quorum":
		return fmt.Sprintf(
			"%s  %s collection has insufficient sources (%d/%d).",
			prefix,
			diagnosticASCII(record.Symbol),
			record.SuccessfulSourceCount,
			record.ConfiguredSourceCount,
		)
	case "collection_recovered":
		return fmt.Sprintf(
			"%s  %s collection recovered (%d/%d sources).",
			prefix,
			diagnosticASCII(record.Symbol),
			record.SuccessfulSourceCount,
			record.ConfiguredSourceCount,
		)
	case "consumer_omission":
		return fmt.Sprintf(
			"%s  Node request omitted %s: %s.",
			prefix,
			diagnosticASCII(record.Symbol),
			diagnosticOmission(record.Reason),
		)
	default:
		return prefix + "  Oracle diagnostic event."
	}
}

func diagnosticOmission(reason string) string {
	switch reason {
	case "unconfigured":
		return "the symbol is not configured"
	case "no_value":
		return "no aggregate is available"
	case "stale":
		return "the aggregate is stale"
	case "clock_anomaly":
		return "the aggregate has a clock anomaly"
	default:
		return "no usable aggregate is available"
	}
}

func diagnosticASCII(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return strconv.QuoteToASCII(value)
		}
	}
	return value
}

func diagnosticShellQuote(value string) string {
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

func diagnosticCommand(home, command string) string {
	if home == "" {
		return "oracled " + command
	}
	return "oracled --home " + diagnosticShellQuote(home) + " " + command
}

func diagnosticLevel(level string) int {
	switch level {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn":
		return 2
	default:
		return 3
	}
}
