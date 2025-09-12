package keeper

import (
	"github.com/GPTx-global/guru-v2/v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ types.OracleHooks = MultiOracleHooks{}

// combine multiple epoch hooks, all hook functions are run in array sequence
type MultiOracleHooks []types.OracleHooks

func NewMultiOracleHooks(hooks ...types.OracleHooks) MultiOracleHooks {
	return hooks
}

// AfterEpochEnd is called when epoch is going to be ended, epochNumber is the
// number of epoch that is ending
func (mh MultiOracleHooks) AfterOracleEnd(ctx sdk.Context, dataSet types.DataSet) {
	for i := range mh {
		mh[i].AfterOracleEnd(ctx, dataSet)
	}
}

// BeforeEpochStart is called when epoch is going to be started, epochNumber is
// the number of epoch that is starting
func (mh MultiOracleHooks) BeforeOracleStart(ctx sdk.Context, dataSet types.DataSet) {
	for i := range mh {
		mh[i].BeforeOracleStart(ctx, dataSet)
	}
}

// AfterEpochEnd executes the indicated hook after epochs ends
func (k Keeper) AfterOracleEnd(ctx sdk.Context, dataSet types.DataSet) {
	k.hooks.AfterOracleEnd(ctx, dataSet)
}

// BeforeEpochStart executes the indicated hook before the epochs
func (k Keeper) BeforeOracleStart(ctx sdk.Context, dataSet types.DataSet) {
	k.hooks.BeforeOracleStart(ctx, dataSet)
}
