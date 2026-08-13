package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func (k Keeper) SetTask(ctx context.Context, task *types.OracleTask) error {
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
func (k Keeper) SetTaskDefinition(ctx context.Context, task *types.OracleTask) error {
	if err := ValidateTask(task); err != nil {
		return err
	}

	task.Symbol = NormalizeSymbol(task.GetSymbol())
	return k.setTaskDefinition(ctx, task)
}

func (k Keeper) setTaskDefinition(ctx context.Context, task *types.OracleTask) error {
	return k.tasks.Set(ctx, task.GetSymbol(), *task)
}

func (k Keeper) GetTask(ctx context.Context, symbol string) (*types.OracleTask, error) {
	task, err := k.tasks.Get(ctx, NormalizeSymbol(symbol))
	if err != nil {
		return nil, err
	}

	return &task, nil
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

func (k Keeper) ListTasks(ctx context.Context, enabledOnly bool) ([]*types.OracleTask, error) {
	tasks := []*types.OracleTask{}
	err := k.tasks.Walk(ctx, nil, func(_ string, task types.OracleTask) (bool, error) {
		if enabledOnly && !task.GetEnabled() {
			return false, nil
		}
		taskCopy := task
		tasks = append(tasks, &taskCopy)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func (k Keeper) SetTaskSchedule(ctx context.Context, entry *types.OracleTaskScheduleEntry) error {
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

func (k Keeper) ListTaskSchedule(ctx context.Context) ([]*types.OracleTaskScheduleEntry, error) {
	schedule := []*types.OracleTaskScheduleEntry{}
	err := k.taskSchedule.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		schedule = append(schedule, &types.OracleTaskScheduleEntry{
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

func (k Keeper) DueTasks(ctx context.Context, height int64) ([]*types.OracleTask, error) {
	if height <= 0 {
		return nil, nil
	}

	tasks := []*types.OracleTask{}
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
