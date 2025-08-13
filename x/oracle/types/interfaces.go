package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// OracleHooks event hooks for oracle processing
type OracleHooks interface {
	AfterOracleEnd(ctx sdk.Context, dataSet DataSet)
	BeforeOracleStart(ctx sdk.Context, dataSet DataSet)
}
