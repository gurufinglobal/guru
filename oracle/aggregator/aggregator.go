package aggregator

import (
	"context"
	"fmt"
	"math/big"
	"runtime"
	"slices"
	"strings"
	"sync"

	"cosmossdk.io/log"
	"github.com/creachadair/taskgroup"
	"github.com/gurufinglobal/guru/v2/oracle/provider"
	"github.com/gurufinglobal/guru/v2/oracle/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type providerSample struct {
	provider string
	raw      string
	val      *big.Rat
}

type Aggregator struct {
	logger      log.Logger
	pvRegistry  *provider.Registry
	workerGroup *taskgroup.Group
	workerFunc  taskgroup.StartFunc
	done        chan struct{}
	startOnce   sync.Once
}

func NewAggregator(logger log.Logger, pvRegistry *provider.Registry) *Aggregator {
	workerGroup, workerFunc := taskgroup.New(nil).Limit(2 * runtime.NumCPU())
	return &Aggregator{
		logger:      logger,
		pvRegistry:  pvRegistry,
		workerGroup: workerGroup,
		workerFunc:  workerFunc,
		done:        make(chan struct{}),
	}
}

func (a *Aggregator) Start(ctx context.Context, taskChan <-chan types.OracleTask, resultCh chan<- oracletypes.OracleReport) {
	a.startOnce.Do(func() {
		go func() {
			defer close(a.done)
			for {
				select {
				case <-ctx.Done():
					a.logger.Info("aggregator shutting down, waiting for active tasks to complete")
					if err := a.workerGroup.Wait(); err != nil {
						a.logger.Error("error during aggregator shutdown", "error", err)
					} else {
						a.logger.Info("aggregator shutdown completed successfully")
					}
					return
				case task, ok := <-taskChan:
					if !ok {
						a.logger.Info("task channel closed, waiting for active tasks to complete")
						if err := a.workerGroup.Wait(); err != nil {
							a.logger.Error("error during aggregator shutdown", "error", err)
						} else {
							a.logger.Info("aggregator shutdown completed successfully")
						}
						return
					}

					t := task
					a.workerFunc(func() error {
						a.processTask(ctx, t, resultCh)
						return nil
					})
				}
			}
		}()
	})
}

// Done is closed when the aggregator main loop exits.
func (a *Aggregator) Done() <-chan struct{} { return a.done }

// processTask fetches data from providers for a single task and emits result.
func (a *Aggregator) processTask(ctx context.Context, task types.OracleTask, resultCh chan<- oracletypes.OracleReport) {
	providers := a.pvRegistry.GetProviders(int32(task.Category))
	if len(providers) == 0 {
		// This should not happen if registry validates category >= 1 provider.
		a.logger.Error("no providers for category", "request_id", task.Id, "category", task.Category, "symbol", task.Symbol)
		return
	}

	var (
		wg      sync.WaitGroup
		results = make([]*providerSample, len(providers))
	)

	for i, pv := range providers {
		wg.Add(1)
		go func(idx int, pv provider.Provider) {
			defer wg.Done()
			raw, err := pv.Fetch(ctx, task.Symbol)
			if err != nil {
				a.logger.Debug("provider fetch failed",
					"error", err,
					"provider", pv.ID(),
					"request_id", task.Id,
					"category", task.Category,
					"symbol", task.Symbol,
				)
				return
			}

			rat, err := parseChainDecimalToRat(raw)
			if err != nil {
				a.logger.Debug("provider returned invalid decimal",
					"error", err,
					"provider", pv.ID(),
					"request_id", task.Id,
					"category", task.Category,
					"symbol", task.Symbol,
					"raw_data", raw,
				)
				return
			}

			results[idx] = &providerSample{
				provider: pv.ID(),
				raw:      raw,
				val:      rat,
			}
		}(i, pv)
	}

	wg.Wait()

	successCount := countNonNil(results)
	if successCount == 0 {
		a.logger.Error("all provider fetches failed", "request_id", task.Id, "category", task.Category, "symbol", task.Symbol)
		return
	}

	median := selectMiddleValue(results)
	if median == nil {
		a.logger.Error("median selection failed", "request_id", task.Id, "category", task.Category, "symbol", task.Symbol)
		return
	}

	a.logger.Info("median selected",
		"request_id", task.Id,
		"category", task.Category,
		"symbol", task.Symbol,
		"raw_data", median.raw,
		"source_provider", median.provider,
		"success_count", successCount,
		"total_providers", len(providers),
	)

	if resultCh == nil {
		return
	}

	select {
	case <-ctx.Done():
		a.logger.Debug("context canceled before emitting result", "request_id", task.Id, "category", task.Category, "symbol", task.Symbol)
	case resultCh <- oracletypes.OracleReport{
		RequestId: task.Id,
		RawData:   median.raw,
		Provider:  "",
		Nonce:     task.Nonce,
		Signature: nil,
	}:
	}
}

// selectMiddleValue returns the middle element (lower median for even length) without averaging.
func selectMiddleValue(values []*providerSample) *providerSample {
	if len(values) == 0 {
		return nil
	}

	valid := make([]*providerSample, 0, len(values))
	for _, v := range values {
		if v != nil {
			valid = append(valid, v)
		}
	}
	if len(valid) == 0 {
		return nil
	}

	sorted := make([]*providerSample, len(valid))
	copy(sorted, valid)

	slices.SortFunc(sorted, func(a, b *providerSample) int {
		return a.val.Cmp(b.val)
	})

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return sorted[mid-1]
	}
	return sorted[mid]
}

func countNonNil(values []*providerSample) int {
	count := 0
	for _, v := range values {
		if v != nil {
			count++
		}
	}
	return count
}

func parseChainDecimalToRat(raw string) (*big.Rat, error) {
	if raw == "" {
		return nil, fmt.Errorf("raw_data is empty")
	}
	// Chain validation is big.Float.SetString (decimal only).
	if _, ok := new(big.Float).SetString(raw); !ok {
		return nil, fmt.Errorf("raw_data is not a valid decimal")
	}
	// big.Rat also accepts fractions like "1/3" which chain does not; reject explicitly.
	if strings.Contains(raw, "/") {
		return nil, fmt.Errorf("raw_data must be a decimal, not a fraction")
	}
	r, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("failed to parse raw_data as rational")
	}
	return r, nil
}
