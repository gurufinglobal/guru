package ibctesting

import (
	"encoding/json"

	"github.com/GPTx-global/guru-v2/v2/gurud"
	feemarkettypes "github.com/GPTx-global/guru-v2/v2/x/feemarket/types"
	dbm "github.com/cosmos/cosmos-db"
	ibctesting "github.com/cosmos/ibc-go/v10/testing"

	"cosmossdk.io/log"

	"github.com/GPTx-global/guru-v2/v2/ibc/simapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
)

func SetupExampleApp() (ibctesting.TestingApp, map[string]json.RawMessage) {
	app := gurud.NewExampleApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		nil,
		true,
		simtestutil.EmptyAppOptions{},
		9001,
		gurud.EvmAppOptions,
	)
	// disable base fee for testing
	genesisState := app.DefaultGenesis()
	fmGen := feemarkettypes.DefaultGenesisState()
	fmGen.Params.NoBaseFee = true
	genesisState[feemarkettypes.ModuleName] = app.AppCodec().MustMarshalJSON(fmGen)

	return app, genesisState
}

func SetupTestingApp() (ibctesting.TestingApp, map[string]json.RawMessage) {
	db := dbm.NewMemDB()
	app := simapp.NewSimApp(log.NewNopLogger(), db, nil, true, simtestutil.EmptyAppOptions{})
	return app, app.DefaultGenesis()
}
