package keeper

import (
	"context"
	"strings"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

func NormalizeSymbol(symbol string) string {
	return strings.TrimSpace(symbol)
}

func (k Keeper) SetTask(ctx context.Context, task *oraclev1.OracleTask) error {
	if err := ValidateTask(task); err != nil {
		return err
	}

	task.Symbol = NormalizeSymbol(task.GetSymbol())
	return k.tasks.Set(ctx, task.Symbol, task)
}

func (k Keeper) GetTask(ctx context.Context, symbol string) (*oraclev1.OracleTask, error) {
	return k.tasks.Get(ctx, NormalizeSymbol(symbol))
}

func (k Keeper) RemoveTask(ctx context.Context, symbol string) error {
	normalized := NormalizeSymbol(symbol)
	if normalized == "" {
		return types.ErrInvalidTask.Wrap("symbol cannot be empty")
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

	return nil
}
