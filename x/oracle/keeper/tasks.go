package keeper

import (
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

func NormalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func (k Keeper) SetTask(ctx context.Context, task *oraclev1.OracleTask) error {
	if err := ValidateTask(task); err != nil {
		return err
	}

	task.Symbol = NormalizeSymbol(task.GetSymbol())
	if err := k.removeScheduledTask(ctx, task.Symbol); err != nil {
		return err
	}
	if err := k.tasks.Set(ctx, task.Symbol, task); err != nil {
		return err
	}
	if !task.GetEnabled() {
		return nil
	}

	return k.seedTaskSchedule(ctx, task, sdk.UnwrapSDKContext(ctx).BlockHeight())
}

func (k Keeper) GetTask(ctx context.Context, symbol string) (*oraclev1.OracleTask, error) {
	return k.tasks.Get(ctx, NormalizeSymbol(symbol))
}

func (k Keeper) RemoveTask(ctx context.Context, symbol string) error {
	normalized := NormalizeSymbol(symbol)
	if normalized == "" {
		return types.ErrInvalidTask.Wrap("symbol cannot be empty")
	}
	if err := k.removeScheduledTask(ctx, normalized); err != nil {
		return err
	}
	return k.tasks.Remove(ctx, normalized)
}

func (k Keeper) ListTasks(ctx context.Context, enabledOnly bool) ([]*oraclev1.OracleTask, error) {
	tasks := []*oraclev1.OracleTask{}
	err := k.tasks.Walk(ctx, nil, func(_ string, task *oraclev1.OracleTask) (bool, error) {
		if enabledOnly && !task.GetEnabled() {
			return false, nil
		}
		tasks = append(tasks, task)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func (k Keeper) DueTasks(ctx context.Context, height int64) ([]*oraclev1.OracleTask, error) {
	if height <= 0 {
		return nil, nil
	}

	tasks := []*oraclev1.OracleTask{}
	err := k.taskSchedule.Walk(ctx, collections.NewPrefixedPairRange[int64, string](height), func(key collections.Pair[int64, string]) (bool, error) {
		task, err := k.GetTask(ctx, key.K2())
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		if task.GetEnabled() {
			tasks = append(tasks, task)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func (k Keeper) AdvanceTaskSchedule(ctx context.Context, height int64) error {
	if height <= 0 {
		return nil
	}

	symbols, err := k.scheduledSymbolsAt(ctx, height)
	if err != nil {
		return err
	}
	if len(symbols) == 0 {
		return nil
	}

	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if currentHeight < height {
		currentHeight = height
	}
	for _, symbol := range symbols {
		if err := k.removeScheduledTaskAt(ctx, symbol, height); err != nil {
			return err
		}
		task, err := k.GetTask(ctx, symbol)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return err
		}
		if !task.GetEnabled() {
			continue
		}
		if err := k.scheduleNextTaskHeights(ctx, task, height, currentHeight); err != nil {
			return err
		}
	}

	return nil
}

func ValidateTask(task *oraclev1.OracleTask) error {
	if task == nil {
		return types.ErrInvalidTask.Wrap("task cannot be nil")
	}
	if NormalizeSymbol(task.GetSymbol()) == "" {
		return types.ErrInvalidTask.Wrap("symbol cannot be empty")
	}
	if task.GetValueType() == oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED {
		return types.ErrInvalidTask.Wrap("value_type cannot be unspecified")
	}
	if task.GetSubmissionInterval() == 0 {
		return types.ErrInvalidTask.Wrap("submission_interval must be positive")
	}

	return nil
}

func nextSubmissionHeight(currentHeight int64, interval uint32) int64 {
	if currentHeight < 0 {
		currentHeight = 0
	}

	return currentHeight + int64(interval)
}

func (k Keeper) seedTaskSchedule(ctx context.Context, task *oraclev1.OracleTask, currentHeight int64) error {
	height := nextSubmissionHeight(currentHeight, task.GetSubmissionInterval())
	if err := k.scheduleTaskAt(ctx, task.GetSymbol(), height); err != nil {
		return err
	}

	return k.scheduleTaskAt(ctx, task.GetSymbol(), height+int64(task.GetSubmissionInterval()))
}

func (k Keeper) scheduleNextTaskHeights(ctx context.Context, task *oraclev1.OracleTask, lastDueHeight int64, currentHeight int64) error {
	interval := int64(task.GetSubmissionInterval())
	nextHeight := lastDueHeight + interval
	for nextHeight <= currentHeight {
		nextHeight += interval
	}
	if err := k.scheduleTaskAt(ctx, task.GetSymbol(), nextHeight); err != nil {
		return err
	}

	return k.scheduleTaskAt(ctx, task.GetSymbol(), nextHeight+interval)
}

func (k Keeper) scheduleTaskAt(ctx context.Context, symbol string, height int64) error {
	if err := k.taskSchedule.Set(ctx, collections.Join(height, symbol)); err != nil {
		return err
	}

	return k.taskScheduleBySymbol.Set(ctx, collections.Join(symbol, height))
}

func (k Keeper) removeScheduledTask(ctx context.Context, symbol string) error {
	heights, err := k.scheduledHeightsForSymbol(ctx, symbol)
	if err != nil {
		return err
	}

	for _, height := range heights {
		if err := k.removeScheduledTaskAt(ctx, symbol, height); err != nil {
			return err
		}
	}

	return nil
}

func (k Keeper) removeScheduledTaskAt(ctx context.Context, symbol string, height int64) error {
	if err := k.taskSchedule.Remove(ctx, collections.Join(height, symbol)); err != nil {
		return err
	}

	return k.taskScheduleBySymbol.Remove(ctx, collections.Join(symbol, height))
}

func (k Keeper) scheduledSymbolsAt(ctx context.Context, height int64) ([]string, error) {
	symbols := []string{}
	err := k.taskSchedule.Walk(ctx, collections.NewPrefixedPairRange[int64, string](height), func(key collections.Pair[int64, string]) (bool, error) {
		symbols = append(symbols, key.K2())
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return symbols, nil
}

func (k Keeper) scheduledHeightsForSymbol(ctx context.Context, symbol string) ([]int64, error) {
	heights := []int64{}
	err := k.taskScheduleBySymbol.Walk(ctx, collections.NewPrefixedPairRange[string, int64](symbol), func(key collections.Pair[string, int64]) (bool, error) {
		heights = append(heights, key.K2())
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return heights, nil
}
