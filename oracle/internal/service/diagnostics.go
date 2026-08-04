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
	diagnostics := &Diagnostics{
		output:  output,
		format:  logging.Format,
		minimum: diagnosticLevel(logging.Level),
		now:     now,
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
		var builder strings.Builder
		fmt.Fprintf(
			&builder,
			"%s level=%s event=%s",
			record.Timestamp,
			record.Level,
			record.Event,
		)
		if record.Symbol != "" {
			fmt.Fprintf(&builder, " symbol=%s", strconv.Quote(record.Symbol))
		}
		if record.Reason != "" {
			fmt.Fprintf(&builder, " reason=%s", strconv.Quote(record.Reason))
		}
		if record.Event == "ready" {
			fmt.Fprintf(&builder, " feed_count=%d", record.FeedCount)
		}
		if strings.HasPrefix(record.Event, "collection_") {
			fmt.Fprintf(&builder, " configured_source_count=%d", record.ConfiguredSourceCount)
			fmt.Fprintf(&builder, " successful_source_count=%d", record.SuccessfulSourceCount)
		}
		line = []byte(builder.String())
	}
	if len(line) >= maxDiagnosticBytes {
		line = line[:maxDiagnosticBytes-1]
	}
	line = append(line, '\n')
	_, _ = d.output.Write(line)
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
