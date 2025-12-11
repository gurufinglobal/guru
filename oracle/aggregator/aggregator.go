package aggregator

import (
	"context"
	"math/big"
	"sort"
	"sync"

	"cosmossdk.io/log"
	"github.com/gurufinglobal/guru/v2/oracle/provider"
	"github.com/gurufinglobal/guru/v2/oracle/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type Aggregator struct {
	logger     log.Logger
	pvRegistry *provider.Registry
}

func NewAggregator(logger log.Logger, pvRegistry *provider.Registry) *Aggregator {
	return &Aggregator{
		logger:     logger,
		pvRegistry: pvRegistry,
	}
}

func (a *Aggregator) Start(ctx context.Context, taskChan <-chan types.OracleTask, resultCh chan<- oracletypes.OracleReport) {
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-taskChan:
			if !ok {
				return
			}

			// 각 Task 처리를 별도 goroutine으로 실행해 병렬 처리 가능하게 함.
			go a.processTask(ctx, task, resultCh)
		}
	}
}

// processTask fetches data from providers for a single task and emits result.
func (a *Aggregator) processTask(ctx context.Context, task types.OracleTask, resultCh chan<- oracletypes.OracleReport) {
	providers := a.pvRegistry.GetProviders(int32(task.Category))
	if len(providers) == 0 {
		a.logger.Warn("no providers for category", "category", task.Category)
		return
	}

	var (
		wg      sync.WaitGroup
		results = make([]*big.Float, len(providers))
	)

	for i, pv := range providers {
		wg.Add(1)
		go func(idx int, pv provider.Provider) {
			defer wg.Done()
			val, err := pv.Fetch(ctx, task.Symbol)
			if err != nil {
				a.logger.Error("failed to fetch price", "error", err, "provider", pv.ID(), "symbol", task.Symbol)
				return
			}

			results[idx] = val
		}(i, pv)
	}

	wg.Wait()

	successCount := countNonNil(results)
	if successCount == 0 {
		a.logger.Error("all provider requests failed", "symbol", task.Symbol, "category", task.Category)
		return
	}

	median := selectMiddleValue(results)
	if median == nil {
		a.logger.Error("failed to select middle value", "symbol", task.Symbol, "category", task.Category)
		return
	}

	a.logger.Info("selected middle value", "symbol", task.Symbol, "category", task.Category, "value", median.Text('f', -1), "success_count", successCount, "total_providers", len(providers))

	if resultCh == nil {
		return
	}

	select {
	case <-ctx.Done():
		a.logger.Debug("context canceled before emitting result", "symbol", task.Symbol, "category", task.Category)
	case resultCh <- oracletypes.OracleReport{
		RequestId: task.Id,
		RawData:   median.Text('f', -1),
		Provider:  "",
		Nonce:     task.Nonce,
		Signature: nil,
	}:
	}
}

// selectMiddleValue returns the middle element (lower median for even length) without averaging.
func selectMiddleValue(values []*big.Float) *big.Float {
	if len(values) == 0 {
		return nil
	}

	valid := make([]*big.Float, 0, len(values))
	for _, v := range values {
		if v != nil {
			valid = append(valid, v)
		}
	}
	if len(valid) == 0 {
		return nil
	}

	sorted := make([]*big.Float, len(valid))
	copy(sorted, valid)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Cmp(sorted[j]) < 0
	})

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return sorted[mid-1]
	}
	return sorted[mid]
}

func countNonNil(values []*big.Float) int {
	count := 0
	for _, v := range values {
		if v != nil {
			count++
		}
	}
	return count
}
