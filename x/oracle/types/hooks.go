package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type OracleHooks interface {
	AfterOracleAggregation(ctx sdk.Context, request OracleRequest, result OracleResult)
}

type MultiOracleHooks []OracleHooks

func NewMultiOracleHooks(hooks ...OracleHooks) MultiOracleHooks {
	return hooks
}

func (mh MultiOracleHooks) AfterOracleAggregation(ctx sdk.Context, request OracleRequest, result OracleResult) {
	for i := range mh {
		mh[i].AfterOracleAggregation(ctx, request, result)
	}
}
