package keeper

import (
	"context"
	"errors"
	"sort"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func (k Keeper) DueTasksForVoteExtension(ctx context.Context, height int64) ([]*oraclev1.OracleTask, error) {
	dueBySymbol, err := k.dueTaskScheduleBySymbol(ctx, height)
	if err != nil {
		return nil, err
	}
	if len(dueBySymbol) == 0 {
		return nil, nil
	}

	symbols := sortedSymbols(dueBySymbol)
	tasks := make([]*oraclev1.OracleTask, 0, len(symbols))
	for _, symbol := range symbols {
		task, err := k.GetTask(ctx, symbol)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if task.GetEnabled() {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (k Keeper) AdvanceTaskSchedule(ctx context.Context, height int64) error {
	if height <= 0 {
		return nil
	}

	dueBySymbol, err := k.dueTaskScheduleBySymbol(ctx, height)
	if err != nil {
		return err
	}
	if len(dueBySymbol) == 0 {
		return nil
	}

	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if currentHeight < height {
		currentHeight = height
	}
	symbols := sortedSymbols(dueBySymbol)
	for _, symbol := range symbols {
		if err := k.removeDueTaskSchedule(ctx, symbol, height); err != nil {
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
		if err := k.fillTaskScheduleWindow(ctx, task, dueBySymbol[symbol], currentHeight); err != nil {
			return err
		}
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

func (k Keeper) fillTaskScheduleWindow(ctx context.Context, task *oraclev1.OracleTask, lastDueHeight int64, currentHeight int64) error {
	heights, err := k.scheduledHeightsForSymbol(ctx, task.GetSymbol())
	if err != nil {
		return err
	}

	scheduled := make(map[int64]struct{}, len(heights))
	for _, height := range heights {
		scheduled[height] = struct{}{}
	}

	// Keep exactly two future submissions per symbol so missed intervals do not
	// trigger unbounded schedule scans after validator downtime.
	interval := int64(task.GetSubmissionInterval())
	nextHeight := lastDueHeight + interval
	for nextHeight <= currentHeight {
		nextHeight += interval
	}

	for len(scheduled) < 2 {
		if _, exists := scheduled[nextHeight]; !exists {
			if err := k.scheduleTaskAt(ctx, task.GetSymbol(), nextHeight); err != nil {
				return err
			}
			scheduled[nextHeight] = struct{}{}
		}
		nextHeight += interval
	}

	return nil
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

func (k Keeper) dueTaskScheduleBySymbol(ctx context.Context, height int64) (map[string]int64, error) {
	dueBySymbol := map[string]int64{}
	if height <= 0 {
		return dueBySymbol, nil
	}

	err := k.taskSchedule.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		scheduledHeight := key.K1()
		if scheduledHeight >= height-1 {
			return true, nil
		}
		addDueTaskSchedule(dueBySymbol, key.K2(), scheduledHeight)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	err = k.taskSchedule.Walk(ctx, collections.NewPrefixedPairRange[int64, string](height), func(key collections.Pair[int64, string]) (bool, error) {
		addDueTaskSchedule(dueBySymbol, key.K2(), key.K1())
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return dueBySymbol, nil
}

func addDueTaskSchedule(dueBySymbol map[string]int64, symbol string, height int64) {
	if existing, ok := dueBySymbol[symbol]; !ok || height > existing {
		dueBySymbol[symbol] = height
	}
}

func sortedSymbols(bySymbol map[string]int64) []string {
	symbols := make([]string, 0, len(bySymbol))
	for symbol := range bySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func (k Keeper) removeDueTaskSchedule(ctx context.Context, symbol string, height int64) error {
	heights, err := k.scheduledHeightsForSymbol(ctx, symbol)
	if err != nil {
		return err
	}

	for _, scheduledHeight := range heights {
		if !isDueTaskScheduleHeight(scheduledHeight, height) {
			continue
		}
		if err := k.removeScheduledTaskAt(ctx, symbol, scheduledHeight); err != nil {
			return err
		}
	}

	return nil
}

func isDueTaskScheduleHeight(scheduledHeight int64, height int64) bool {
	return scheduledHeight == height || scheduledHeight <= height-2
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
