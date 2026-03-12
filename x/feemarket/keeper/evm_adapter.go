package keeper

import (
	cosmosfeemarkettypes "github.com/cosmos/evm/x/feemarket/types"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EVMFeeMarketAdapter bridges guru's custom feemarket keeper to the
// FeeMarketKeeper interface expected by cosmos/evm x/vm.
//
// This adapter allows us to keep using the internal custom Params shape while
// exposing a cosmos-compatible Params view at the vm integration boundary.
type EVMFeeMarketAdapter struct {
	keeper Keeper
}

// NewEVMFeeMarketAdapter creates a vm-facing adapter over the custom feemarket keeper.
func NewEVMFeeMarketAdapter(k Keeper) EVMFeeMarketAdapter {
	return EVMFeeMarketAdapter{keeper: k}
}

// GetBaseFee proxies to the internal keeper.
func (a EVMFeeMarketAdapter) GetBaseFee(ctx sdk.Context) sdkmath.LegacyDec {
	return a.keeper.GetBaseFee(ctx)
}

// CalculateBaseFee proxies to the internal keeper.
func (a EVMFeeMarketAdapter) CalculateBaseFee(ctx sdk.Context) sdkmath.LegacyDec {
	return a.keeper.CalculateBaseFee(ctx)
}

// GetParams converts custom feemarket params to cosmos/evm feemarket params.
//
// NOTE: custom-only fields (e.g. GasPriceAdjustmentFactor, MaxChangeRate) are
// intentionally not part of the cosmos/evm params surface and remain available
// through the internal keeper API.
func (a EVMFeeMarketAdapter) GetParams(ctx sdk.Context) cosmosfeemarkettypes.Params {
	p := a.keeper.GetParams(ctx)
	return cosmosfeemarkettypes.Params{
		NoBaseFee:                p.NoBaseFee,
		BaseFeeChangeDenominator: p.BaseFeeChangeDenominator,
		ElasticityMultiplier:     p.ElasticityMultiplier,
		EnableHeight:             p.EnableHeight,
		BaseFee:                  p.BaseFee,
		MinGasPrice:              p.MinGasPrice,
		MinGasMultiplier:         p.MinGasMultiplier,
	}
}
