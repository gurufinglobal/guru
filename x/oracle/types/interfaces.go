package types

import (
	context "context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// OracleHooks event hooks for oracle processing
type OracleHooks interface {
	AfterOracleEnd(ctx sdk.Context, dataSet DataSet)
	BeforeOracleStart(ctx sdk.Context, dataSet DataSet)
}

type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}
