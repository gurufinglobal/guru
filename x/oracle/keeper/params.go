package keeper

import (
	"context"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

const (
	DefaultMinValidators uint32 = 1
	DefaultMinSources    uint32 = 3
	DefaultHistoryLimit  uint32 = 100
)

func DefaultParams() *oraclev1.Params {
	return &oraclev1.Params{
		MinValidators: DefaultMinValidators,
		MinSources:    DefaultMinSources,
		HistoryLimit:  DefaultHistoryLimit,
	}
}

func (k Keeper) GetParams(ctx context.Context) (*oraclev1.Params, error) {
	return k.params.Get(ctx)
}

func (k Keeper) SetParams(ctx context.Context, params *oraclev1.Params) error {
	if err := ValidateParams(params); err != nil {
		return err
	}
	return k.params.Set(ctx, params)
}

func (k Keeper) UpdateParams(ctx context.Context, params *oraclev1.Params) error {
	return k.SetParams(ctx, params)
}

func ValidateParams(params *oraclev1.Params) error {
	if params == nil {
		return types.ErrInvalidParams.Wrap("params cannot be nil")
	}
	if params.GetMinValidators() == 0 {
		return types.ErrInvalidParams.Wrap("min_validators must be positive")
	}
	if params.GetMinSources() == 0 {
		return types.ErrInvalidParams.Wrap("min_sources must be positive")
	}
	if params.GetHistoryLimit() == 0 {
		return types.ErrInvalidParams.Wrap("history_limit must be positive")
	}

	return nil
}
