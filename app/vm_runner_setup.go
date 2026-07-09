package app

import (
	goruntime "runtime"

	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/baseapp/txnrunner"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	vmrunner "github.com/cosmos/evm/x/vm/runner"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
)

func (app *App) configureVMRunner(
	bApp *baseapp.BaseApp,
	txDecoder sdk.TxDecoder,
	nonTransientKeys []storetypes.StoreKey,
) {
	vmrunner.SetRunner(bApp, oracleabci.NewPayloadSkippingTxRunner(txnrunner.NewSTMRunner(
		txDecoder,
		nonTransientKeys,
		min(goruntime.GOMAXPROCS(0), goruntime.NumCPU()),
		true,
		func(ms storetypes.MultiStore) string {
			denom := app.EVMKeeper.GetParams(
				sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger()),
			).EvmDenom
			if denom == "" {
				return appparams.BaseDenom
			}
			return denom
		},
	)))
}
