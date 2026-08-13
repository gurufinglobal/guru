package keeper

import (
	"strings"

	"github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func NormalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func ValidateTaskScheduleEntry(entry *types.OracleTaskScheduleEntry) error {
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

func ValidateTask(task *types.OracleTask) error {
	if task == nil {
		return types.ErrInvalidTask.Wrap("task cannot be nil")
	}
	if NormalizeSymbol(task.GetSymbol()) == "" {
		return types.ErrInvalidTask.Wrap("symbol cannot be empty")
	}
	if task.GetValueType() == types.ValueType_VALUE_TYPE_UNSPECIFIED {
		return types.ErrInvalidTask.Wrap("value_type cannot be unspecified")
	}
	if task.GetValueType() != types.ValueType_VALUE_TYPE_NUMERIC {
		return types.ErrInvalidTask.Wrap("non-numeric value_type is not supported")
	}
	if task.GetSubmissionInterval() == 0 {
		return types.ErrInvalidTask.Wrap("submission_interval must be positive")
	}

	return nil
}
