package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker is called at the beginning of every block
func (k Keeper) BeginBlocker(ctx sdk.Context) {
	params := k.GetParams(ctx)
	if !params.EnableOracle {
		k.Logger(ctx).Info("oracle is disabled, skipping BeginBlocker")
		return
	}

	k.Logger(ctx).Info("oracle BeginBlocker started")
	k.ProcessOracleDataSetAggregation(ctx)
}
