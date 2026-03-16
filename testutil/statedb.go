package testutil

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/vm/statedb"
	anteinterfaces "github.com/gurufinglobal/guru/v2/ante/interfaces"
)

// NewStateDB returns a new StateDB for testing purposes.
func NewStateDB(ctx sdk.Context, evmKeeper anteinterfaces.EVMKeeper) *statedb.StateDB {
	return statedb.New(ctx, evmKeeper, statedb.NewEmptyTxConfig())
}
