package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"cosmossdk.io/log/v2"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	evmaddress "github.com/cosmos/evm/encoding/address"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestOracleModuleWiringAndAppBoot(t *testing.T) {
	app := NewApp(log.NewNopLogger(), dbm.NewMemDB(), true, sims.EmptyAppOptions{}, baseapp.SetChainID(appparams.SDKChainID))

	require.Contains(t, app.ModuleManager.Modules, oracletypes.ModuleName)
	require.NotNil(t, app.OracleProposalHandler)

	genesis := app.BuildChainDefaultGenesis()
	var oracleGenesis oraclev1.GenesisState
	require.NoError(t, app.AppCodec().UnmarshalJSON(genesis[oracletypes.ModuleName], &oracleGenesis))
	require.True(t, oracleGenesis.GetParams().GetEnabled())
	require.Equal(t, uint32(1), oracleGenesis.GetParams().GetMinValidators())
	require.Equal(t, uint32(3), oracleGenesis.GetParams().GetMinSources())
	require.Equal(t, uint32(100), oracleGenesis.GetParams().GetHistoryLimit())

	setConstitutionGenesisAddresses(t, app, genesis)
	require.NoError(t, app.ValidateChainGenesis(genesis))
}

func setConstitutionGenesisAddresses(t *testing.T, app *App, genesis map[string]json.RawMessage) {
	t.Helper()

	var constitutionGenesis constitutionv1.GenesisState
	require.NoError(t, app.AppCodec().UnmarshalJSON(genesis[constitutiontypes.ModuleName], &constitutionGenesis))
	constitutionGenesis.BaseAddress = oracleWiringAddress(t, 0x11)
	constitutionGenesis.ModeratorAddress = oracleWiringAddress(t, 0x22)
	genesis[constitutiontypes.ModuleName] = app.AppCodec().MustMarshalJSON(&constitutionGenesis)
}

func oracleWiringAddress(t *testing.T, b byte) string {
	t.Helper()

	address, err := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr).BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}
