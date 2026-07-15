package keeper

import (
	"context"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func DefaultParams() *transwapv1.Params {
	return types.DefaultParams()
}

func ValidateParams(params *transwapv1.Params) error {
	return types.ValidateParams(params)
}

func (k Keeper) GetParams(ctx context.Context) (*transwapv1.Params, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get([]byte(types.ParamsKey))
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		// Lazy defaults preserve startup compatibility for chains whose transwap
		// store predates the refund retry parameters.
		return DefaultParams(), nil
	}
	params := &transwapv1.Params{}
	if err := k.cdc.Unmarshal(bz, params); err != nil {
		return nil, err
	}
	if err := ValidateParams(params); err != nil {
		return nil, err
	}
	return params, nil
}

func (k Keeper) SetParams(ctx context.Context, params *transwapv1.Params) error {
	if err := ValidateParams(params); err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	return store.Set([]byte(types.ParamsKey), k.cdc.MustMarshal(params))
}
