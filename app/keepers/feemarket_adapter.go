package keepers

import (
	"context"
	"fmt"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
)

// feeMarketParamsStore is the v0.6.1 FeeMarket parameter surface used by the
// application adapter. Keeping this boundary private prevents economic-policy
// modules from depending on the concrete Cosmos EVM keeper.
type feeMarketParamsStore interface {
	GetParams(ctx sdk.Context) feemarkettypes.Params
	SetParams(ctx sdk.Context, params feemarkettypes.Params) error
}

// FeeMarketAdapter exposes only the FeeMarket state owned by Guru's economic
// policy. The upstream keeper remains the source of truth for all parameters.
type FeeMarketAdapter struct {
	paramsStore feeMarketParamsStore
}

func newFeeMarketAdapter(paramsStore feeMarketParamsStore) FeeMarketAdapter {
	return FeeMarketAdapter{paramsStore: paramsStore}
}

// GetMinGasPrice returns the consensus MinGasPrice currently stored by
// FeeMarket.
func (a FeeMarketAdapter) GetMinGasPrice(ctx context.Context) sdkmath.LegacyDec {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return a.paramsStore.GetParams(sdkCtx).MinGasPrice
}

// SetMinGasPrice updates only MinGasPrice and preserves every other FeeMarket
// parameter. Validation happens before the single state write because the
// v0.6.2 keeper's SetParams method does not validate its input.
func (a FeeMarketAdapter) SetMinGasPrice(
	ctx context.Context,
	minGasPrice sdkmath.LegacyDec,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := a.paramsStore.GetParams(sdkCtx)
	params.MinGasPrice = minGasPrice
	if err := params.Validate(); err != nil {
		return fmt.Errorf("validate feemarket params: %w", err)
	}
	if err := a.paramsStore.SetParams(sdkCtx, params); err != nil {
		return fmt.Errorf("set feemarket params: %w", err)
	}
	return nil
}
