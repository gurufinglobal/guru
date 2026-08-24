package keeper

import (
	"context"

	"github.com/gurufinglobal/guru/v2/x/oracle/types"
)

const (
	DefaultMinValidators uint32 = 1
	DefaultMinSources    uint32 = 3
	DefaultHistoryLimit  uint32 = 100
)

func DefaultParams() *types.Params {
	return &types.Params{
		MinValidators: DefaultMinValidators,
		MinSources:    DefaultMinSources,
		HistoryLimit:  DefaultHistoryLimit,
	}
}

func (k Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	params, err := k.params.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &params, nil
}

func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if err := ValidateParams(params); err != nil {
		return err
	}
	return k.params.Set(ctx, *params)
}

func (k Keeper) UpdateParams(ctx context.Context, params *types.Params) error {
	return k.SetParams(ctx, params)
}

func ValidateParams(params *types.Params) error {
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
