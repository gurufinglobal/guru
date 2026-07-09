package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

func (k Keeper) GetLatestValue(ctx context.Context, symbol string) (*oraclev1.OracleValue, error) {
	return k.latest.Get(ctx, NormalizeSymbol(symbol))
}

func (k Keeper) ListLatestValues(ctx context.Context) ([]*oraclev1.OracleValue, error) {
	values := []*oraclev1.OracleValue{}
	err := k.latest.Walk(ctx, nil, func(_ string, value *oraclev1.OracleValue) (bool, error) {
		values = append(values, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}

func (k Keeper) GetHistory(ctx context.Context, symbol string) (*oraclev1.OracleHistory, error) {
	return k.history.Get(ctx, NormalizeSymbol(symbol))
}

func (k Keeper) ListHistory(ctx context.Context) ([]*oraclev1.OracleHistory, error) {
	history := []*oraclev1.OracleHistory{}
	err := k.history.Walk(ctx, nil, func(_ string, item *oraclev1.OracleHistory) (bool, error) {
		history = append(history, item)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return history, nil
}

func (k Keeper) ApplyOracleValues(ctx context.Context, values []*oraclev1.OracleValue) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	for _, value := range values {
		if err := k.setOracleValue(ctx, value, params.GetHistoryLimit()); err != nil {
			return err
		}
	}
	if k.hooks != nil {
		for _, value := range values {
			// Pass the task cadence with the finalized value so downstream
			// modules can schedule policy changes without re-walking oracle
			// schedules or depending on local sidecar timing.
			sourceSubmissionInterval := uint32(0)
			task, err := k.GetTask(ctx, value.GetSymbol())
			if err != nil {
				if !isNotFound(err) {
					return err
				}
			} else {
				sourceSubmissionInterval = task.GetSubmissionInterval()
			}
			if err := k.hooks.AfterOracleValueApplied(ctx, value, sourceSubmissionInterval); err != nil {
				return err
			}
		}
	}

	return nil
}

func (k Keeper) SetLatestValue(ctx context.Context, value *oraclev1.OracleValue) error {
	if err := ValidateOracleValue(value); err != nil {
		return err
	}

	value.Symbol = NormalizeSymbol(value.GetSymbol())
	return k.latest.Set(ctx, value.Symbol, value)
}

func (k Keeper) SetHistory(ctx context.Context, history *oraclev1.OracleHistory, historyLimit uint32) error {
	if err := ValidateHistory(history, historyLimit); err != nil {
		return err
	}

	history.Symbol = NormalizeSymbol(history.GetSymbol())
	return k.history.Set(ctx, history.Symbol, history)
}

func (k Keeper) setOracleValue(ctx context.Context, value *oraclev1.OracleValue, historyLimit uint32) error {
	if err := ValidateOracleValue(value); err != nil {
		return err
	}

	value.Symbol = NormalizeSymbol(value.GetSymbol())
	if err := k.latest.Set(ctx, value.Symbol, value); err != nil {
		return err
	}

	history, err := k.history.Get(ctx, value.Symbol)
	if err != nil {
		if !isNotFound(err) {
			return err
		}
		history = &oraclev1.OracleHistory{Symbol: value.Symbol}
	}
	history.Values = append(history.GetValues(), value)
	if limit := int(historyLimit); limit > 0 && len(history.Values) > limit {
		history.Values = history.Values[len(history.Values)-limit:]
	}

	return k.history.Set(ctx, value.Symbol, history)
}

func ValidateOracleValue(value *oraclev1.OracleValue) error {
	if value == nil {
		return types.ErrInvalidValue.Wrap("value cannot be nil")
	}
	if NormalizeSymbol(value.GetSymbol()) == "" {
		return types.ErrInvalidValue.Wrap("symbol cannot be empty")
	}
	if value.GetValueType() == oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED {
		return types.ErrInvalidValue.Wrap("value_type cannot be unspecified")
	}
	if value.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
		return types.ErrInvalidValue.Wrap("non-numeric value_type is not supported")
	}
	if value.GetValue() == "" {
		return types.ErrInvalidValue.Wrap("value cannot be empty")
	}
	if value.GetBlockHeight() < 0 {
		return types.ErrInvalidValue.Wrap("block_height cannot be negative")
	}
	if _, err := sdkmath.LegacyNewDecFromStr(value.GetValue()); err != nil {
		return types.ErrInvalidValue.Wrapf("invalid numeric value: %v", err)
	}

	return nil
}

func ValidateHistory(history *oraclev1.OracleHistory, historyLimit uint32) error {
	if history == nil {
		return types.ErrInvalidValue.Wrap("history cannot be nil")
	}

	symbol := NormalizeSymbol(history.GetSymbol())
	if symbol == "" {
		return types.ErrInvalidValue.Wrap("history symbol cannot be empty")
	}
	if historyLimit > 0 && len(history.GetValues()) > int(historyLimit) {
		return types.ErrInvalidValue.Wrapf("history for %q exceeds history_limit", symbol)
	}
	for _, value := range history.GetValues() {
		if err := ValidateOracleValue(value); err != nil {
			return err
		}
		if NormalizeSymbol(value.GetSymbol()) != symbol {
			return types.ErrInvalidValue.Wrapf("history value symbol %q does not match %q", value.GetSymbol(), symbol)
		}
	}

	return nil
}

func isNotFound(err error) bool {
	return errors.Is(err, collections.ErrNotFound)
}
