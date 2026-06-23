package keeper

import (
	"context"
	"errors"
	"sort"
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
	if err := k.setTaskDefinition(ctx, task); err != nil {
		return err
	}
	if !task.GetEnabled() {
		return nil
	}

	return k.seedTaskSchedule(ctx, task, sdk.UnwrapSDKContext(ctx).BlockHeight())
}

// SetTaskDefinition stores a task without mutating task schedule. It is used
// when restoring task definitions before importing their exported schedule.
func (k Keeper) SetTaskDefinition(ctx context.Context, task *oraclev1.OracleTask) error {
	if err := ValidateTask(task); err != nil {
		return err
	}

	task.Symbol = NormalizeSymbol(task.GetSymbol())
	return k.setTaskDefinition(ctx, task)
}

func (k Keeper) setTaskDefinition(ctx context.Context, task *oraclev1.OracleTask) error {
	return k.tasks.Set(ctx, task.GetSymbol(), task)
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

func (k Keeper) SetTaskSchedule(ctx context.Context, entry *oraclev1.OracleTaskScheduleEntry) error {
	if err := ValidateTaskScheduleEntry(entry); err != nil {
		return err
	}

	symbol := NormalizeSymbol(entry.GetSymbol())
	task, err := k.GetTask(ctx, symbol)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrInvalidTask.Wrapf("task schedule references unknown task %q", symbol)
		}
		return err
	}
	if !task.GetEnabled() {
		return types.ErrInvalidTask.Wrapf("task schedule references disabled task %q", symbol)
	}

	return k.scheduleTaskAt(ctx, symbol, entry.GetHeight())
}

func (k Keeper) ListTaskSchedule(ctx context.Context) ([]*oraclev1.OracleTaskScheduleEntry, error) {
	schedule := []*oraclev1.OracleTaskScheduleEntry{}
	err := k.taskSchedule.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		schedule = append(schedule, &oraclev1.OracleTaskScheduleEntry{
			Symbol: key.K2(),
			Height: key.K1(),
		})
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return schedule, nil
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

func ValidateTaskScheduleEntry(entry *oraclev1.OracleTaskScheduleEntry) error {
	if entry == nil {
		return types.ErrInvalidTask.Wrap("task schedule entry cannot be nil")
	}
	if NormalizeSymbol(entry.GetSymbol()) == "" {
		return types.ErrInvalidTask.Wrap("task schedule symbol cannot be empty")
	}
	if entry.GetHeight() <= 0 {
		return types.ErrInvalidTask.Wrap("task schedule height must be positive")
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
	if task.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
		return types.ErrInvalidTask.Wrap("non-numeric value_type is not supported")
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

func (k Keeper) fillTaskScheduleWindow(ctx context.Context, task *oraclev1.OracleTask, lastDueHeight int64, currentHeight int64) error {
	heights, err := k.scheduledHeightsForSymbol(ctx, task.GetSymbol())
	if err != nil {
		return err
	}

	scheduled := make(map[int64]struct{}, len(heights))
	for _, height := range heights {
		scheduled[height] = struct{}{}
	}

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
