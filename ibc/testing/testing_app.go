package ibctesting

import (
	"encoding/json"

	dbm "github.com/cosmos/cosmos-db"
	ibctesting "github.com/cosmos/ibc-go/v10/testing"

	"cosmossdk.io/log"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	gurudconfig "github.com/gurufinglobal/guru/v2/cmd/gurud/config"
	"github.com/gurufinglobal/guru/v2/gurud"
	"github.com/gurufinglobal/guru/v2/ibc/simapp"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
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

	// Override the EVM genesis denom to use the chain's configured denom
	// instead of the cosmos/evm default ("aatom").
	// Use gurud.NewEVMGenesisState() which also activates all static precompiles
	// and preinstalls; DefaultGenesisState() leaves ActiveStaticPrecompiles empty.
	evmGenesis := gurud.NewEVMGenesisState()
	evmGenesis.Params.EvmDenom = gurudconfig.ExampleChainDenom
	genesisState[evmtypes.ModuleName] = app.AppCodec().MustMarshalJSON(evmGenesis)

	// Add bank denom metadata for the EVM coin so that InitEvmCoinInfo
	// can look up decimals and display denom from the bank module.
	bankGen := banktypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesisState[banktypes.ModuleName], bankGen)
	bankGen.DenomMetadata = append(bankGen.DenomMetadata, banktypes.Metadata{
		Description: "The native EVM, governance and staking token of the Guru chain",
		Base:        gurudconfig.ExampleChainDenom,
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    gurudconfig.ExampleChainDenom,
				Exponent: 0,
			},
			{
				Denom:    gurudconfig.DisplayDenom,
				Exponent: gurudconfig.BaseDenomUnit,
			},
		},
		Name:    "Guru",
		Symbol:  "GXN",
		Display: gurudconfig.DisplayDenom,
	})
	genesisState[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGen)

	return app, genesisState
}

func SetupTestingApp() (ibctesting.TestingApp, map[string]json.RawMessage) {
	db := dbm.NewMemDB()
	app := simapp.NewSimApp(log.NewNopLogger(), db, nil, true, simtestutil.EmptyAppOptions{})
	return app, app.DefaultGenesis()
}
