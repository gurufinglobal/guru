package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

func TestDiagnosticsThrottleTransitionsAndBoundMessages(t *testing.T) {
	var output bytes.Buffer
	now := time.Unix(1_700_000_000, 0)
	diagnostics := newDiagnostics(
		&output,
		config.Logging{Level: "info", Format: "json"},
		func() time.Time { return now },
	)

	diagnostics.CycleFinished("BTC/USD", domain.CycleUnderQuorum, 4, 1)
	diagnostics.CycleFinished("BTC/USD", domain.CycleUnderQuorum, 4, 1)
	now = now.Add(time.Minute)
	diagnostics.CycleFinished("BTC/USD", domain.CycleUnderQuorum, 4, 2)
	diagnostics.CycleFinished("BTC/USD", domain.CycleFull, 4, 4)
	diagnostics.CycleFinished("BTC/USD", domain.CycleFull, 4, 4)
	diagnostics.Omitted("ETH/USD", "unconfigured")
	diagnostics.Omitted("ETH/USD", "unconfigured")
	diagnostics.Omitted("SOL/USD", "unconfigured")
	if err := diagnostics.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("diagnostic line count = %d, want 4:\n%s", len(lines), output.String())
	}
	wantEvents := []string{
		"collection_under_quorum",
		"collection_under_quorum",
		"collection_recovered",
		"consumer_omission",
	}
	for i, line := range lines {
		if len(line) > maxDiagnosticBytes {
			t.Fatalf("diagnostic %d exceeds bound: %d", i, len(line))
		}
		var record diagnosticRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode diagnostic %d: %v", i, err)
		}
		if record.Event != wantEvents[i] {
			t.Fatalf("diagnostic %d event = %q, want %q", i, record.Event, wantEvents[i])
		}
	}
}

func TestConsumerOmissionsUseBoundedDiagnostics(t *testing.T) {
	pair, catalog, _ := serviceFixture(t, time.Now())
	var output bytes.Buffer
	now := time.Now()
	diagnostics := newDiagnostics(
		&output,
		config.Logging{Level: "info", Format: "json"},
		func() time.Time { return now },
	)
	state := newState(pair, catalog, nil, now, diagnostics)

	if values := state.freshAt([]string{"BTC/USD", "ETH/USD"}, now); len(values) != 0 {
		t.Fatalf("unexpected fresh values: %#v", values)
	}
	if values := state.freshAt([]string{"BTC/USD", "ETH/USD"}, now); len(values) != 0 {
		t.Fatalf("unexpected repeated fresh values: %#v", values)
	}
	if err := diagnostics.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	logs := output.String()
	if strings.Count(logs, `"event":"consumer_omission"`) != 2 {
		t.Fatalf("omissions were not independently throttled:\n%s", logs)
	}
	if !strings.Contains(logs, `"reason":"no_value"`) ||
		!strings.Contains(logs, `"reason":"unconfigured"`) {
		t.Fatalf("omission reasons missing:\n%s", logs)
	}
}

func TestStateSuccessfulAggregateEmitsRecovery(t *testing.T) {
	pair, catalog, latest := serviceFixture(t, time.Now())
	var output bytes.Buffer
	now := time.Now()
	diagnostics := newDiagnostics(
		&output,
		config.Logging{Level: "info", Format: "json"},
		func() time.Time { return now },
	)
	state := newState(pair, catalog, latest, now, diagnostics)
	state.CycleFinished("BTC/USD", domain.CycleUnderQuorum, 1, now)
	state.AggregateCommitted(latest["BTC/USD"], domain.CycleFull)
	if err := diagnostics.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	logs := output.String()
	if strings.Count(logs, `"event":"collection_under_quorum"`) != 1 ||
		strings.Count(logs, `"event":"collection_recovered"`) != 1 {
		t.Fatalf("state transition diagnostics are incomplete:\n%s", logs)
	}
}

func TestDiagnosticsCancellationDoesNotEmitRecovery(t *testing.T) {
	var output bytes.Buffer
	diagnostics := NewDiagnostics(
		&output,
		config.Logging{Level: "info", Format: "json"},
	)
	diagnostics.CycleFinished("BTC/USD", domain.CycleUnderQuorum, 4, 1)
	diagnostics.CycleFinished("BTC/USD", domain.CycleCancelled, 4, 0)
	if err := diagnostics.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	logs := output.String()
	if strings.Count(logs, `"event":"collection_under_quorum"`) != 1 ||
		strings.Contains(logs, `"event":"collection_recovered"`) {
		t.Fatalf("cancellation produced an invalid transition:\n%s", logs)
	}
}

func TestConsumerOmissionDoesNotBlockOnDiagnosticOutput(t *testing.T) {
	writer := &blockingDiagnosticWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	diagnostics := NewDiagnostics(writer, config.Logging{Level: "info", Format: "json"})
	pair, catalog, _ := serviceFixture(t, time.Now())
	state := newState(pair, catalog, nil, time.Now(), diagnostics)
	diagnostics.Ready(1)
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("diagnostic writer did not block as expected")
	}

	returned := make(chan struct{})
	go func() {
		for i := 0; i < diagnosticQueueSize*4; i++ {
			_ = state.Fresh([]string{"BTC/USD", "UNKNOWN"})
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("consumer omission path blocked on diagnostic output")
	}

	timeoutContext, timeoutCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer timeoutCancel()
	if err := diagnostics.Close(timeoutContext); err == nil {
		t.Fatal("diagnostic close unexpectedly completed while output was blocked")
	}
	close(writer.release)
	if err := diagnostics.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type blockingDiagnosticWriter struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (w *blockingDiagnosticWriter) Write(content []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(content), nil
}
