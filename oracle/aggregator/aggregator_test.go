package aggregator

import (
	"context"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/gurufinglobal/guru/v2/oracle/provider"
	"github.com/gurufinglobal/guru/v2/oracle/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type mockProvider struct {
	id         string
	categories []int32
	fetchFn    func(ctx context.Context, symbol string) (string, error)
}

func (m mockProvider) ID() string          { return m.id }
func (m mockProvider) Categories() []int32 { return m.categories }
func (m mockProvider) Fetch(ctx context.Context, symbol string) (string, error) {
	return m.fetchFn(ctx, symbol)
}

func TestParseChainDecimalToRat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "valid_int", in: "1", wantErr: false},
		{name: "valid_decimal", in: "1.25", wantErr: false},
		{name: "valid_small", in: "0.0000001", wantErr: false},
		{name: "empty", in: "", wantErr: true},
		{name: "invalid", in: "abc", wantErr: true},
		{name: "fraction_rejected", in: "1/3", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseChainDecimalToRat(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil (value=%v)", got)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestSelectMiddleValue_LowerMedianEven(t *testing.T) {
	t.Parallel()

	samples := []*providerSample{
		mustSample(t, "p1", "1"),
		mustSample(t, "p2", "2"),
		mustSample(t, "p3", "3"),
		mustSample(t, "p4", "4"),
	}

	got := selectMiddleValue(samples)
	if got == nil || got.raw != "2" {
		t.Fatalf("expected lower median raw=2, got %#v", got)
	}
}

func TestProcessTask_EmitsMedianRawData(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	category := int32(2)

	reg, err := provider.New(
		logger,
		[]oracletypes.Category{oracletypes.Category(category)},
		mockProvider{
			id:         "p1",
			categories: []int32{category},
			fetchFn: func(ctx context.Context, symbol string) (string, error) {
				return "1", nil
			},
		},
		mockProvider{
			id:         "p2",
			categories: []int32{category},
			fetchFn: func(ctx context.Context, symbol string) (string, error) {
				return "2", nil
			},
		},
		mockProvider{
			id:         "p3",
			categories: []int32{category},
			fetchFn: func(ctx context.Context, symbol string) (string, error) {
				return "100", nil
			},
		},
	)
	if err != nil {
		t.Fatalf("provider.New error: %v", err)
	}

	a := NewAggregator(logger, reg)
	resultCh := make(chan oracletypes.OracleReport, 1)

	a.processTask(context.Background(), types.OracleTask{
		Id:       7,
		Category: category,
		Symbol:   "BTC/USD",
		Nonce:    9,
	}, resultCh)

	select {
	case got := <-resultCh:
		if got.RequestId != 7 {
			t.Fatalf("expected request_id=7, got %d", got.RequestId)
		}
		if got.RawData != "2" {
			t.Fatalf("expected RawData=2, got %q", got.RawData)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for result")
	}
}

func TestProcessTask_SkipsInvalidProviderValue(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	category := int32(2)

	reg, err := provider.New(
		logger,
		[]oracletypes.Category{oracletypes.Category(category)},
		mockProvider{
			id:         "bad",
			categories: []int32{category},
			fetchFn: func(ctx context.Context, symbol string) (string, error) {
				return "not-a-decimal", nil
			},
		},
		mockProvider{
			id:         "good",
			categories: []int32{category},
			fetchFn: func(ctx context.Context, symbol string) (string, error) {
				return "10", nil
			},
		},
		mockProvider{
			id:         "err",
			categories: []int32{category},
			fetchFn: func(ctx context.Context, symbol string) (string, error) {
				return "", errors.New("boom")
			},
		},
	)
	if err != nil {
		t.Fatalf("provider.New error: %v", err)
	}

	a := NewAggregator(logger, reg)
	resultCh := make(chan oracletypes.OracleReport, 1)

	a.processTask(context.Background(), types.OracleTask{
		Id:       1,
		Category: category,
		Symbol:   "BTC/USD",
		Nonce:    1,
	}, resultCh)

	select {
	case got := <-resultCh:
		if got.RawData != "10" {
			t.Fatalf("expected RawData=10, got %q", got.RawData)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for result")
	}
}

func mustSample(t *testing.T, providerID, raw string) *providerSample {
	t.Helper()
	r, err := parseChainDecimalToRat(raw)
	if err != nil {
		t.Fatalf("parseChainDecimalToRat(%q): %v", raw, err)
	}
	return &providerSample{provider: providerID, raw: raw, val: r}
}

func TestAggregatorStart_WaitsForActiveTasksOnContextCancel(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	category := int32(2)

	startedCh := make(chan struct{})
	blockCh := make(chan struct{})
	reg, err := provider.New(
		logger,
		[]oracletypes.Category{oracletypes.Category(category)},
		mockProvider{
			id:         "blocker",
			categories: []int32{category},
			fetchFn: func(ctx context.Context, symbol string) (string, error) {
				select {
				case <-startedCh:
				default:
					close(startedCh)
				}
				<-blockCh
				return "1", nil
			},
		},
	)
	if err != nil {
		t.Fatalf("provider.New error: %v", err)
	}

	a := NewAggregator(logger, reg)
	taskCh := make(chan types.OracleTask, 1)
	resultCh := make(chan oracletypes.OracleReport, 1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.Start(ctx, taskCh, resultCh)
	}()

	taskCh <- types.OracleTask{Id: 1, Category: category, Symbol: "BTC/USD", Nonce: 1}

	// Ensure the task started (Fetch entered) before cancel, otherwise Start can exit immediately.
	select {
	case <-startedCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for provider fetch to start")
	}

	// Cancel while provider is blocked. Start should not return until blockCh is released.
	cancel()

	select {
	case <-done:
		t.Fatalf("expected Start to block on Wait until active tasks complete")
	case <-time.After(200 * time.Millisecond):
		// ok
	}

	close(blockCh)

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Start to return after unblocking provider")
	}
}
