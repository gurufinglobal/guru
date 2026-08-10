package cmd

import (
	"bytes"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	guruapp "github.com/gurufinglobal/guru/v3/app"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestAppExportLatestHeightLoadsApplication(t *testing.T) {
	const chainID = "guru-export-test-1"
	configureExportTestBech32Prefixes(t)

	logger := log.NewNopLogger()
	db := dbm.NewMemDB()
	appOpts := viper.New()
	appOpts.Set(flags.FlagHome, writeChainIDGenesis(t, chainID))
	appOpts.Set(flags.FlagChainID, chainID)
	appOpts.Set("oracle.enabled", false)
	appOpts.Set(sdkserver.FlagMempoolMaxTxs, -1)

	seedApp := guruapp.NewApp(logger, db, true, appOpts, baseapp.SetChainID(chainID))
	genesis := seedApp.BuildChainDefaultGenesis()
	constitutionGenesis := &constitutiontypes.GenesisState{}
	seedApp.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	addressCodec := seedApp.InterfaceRegistry().SigningContext().AddressCodec()
	baseAddress, err := addressCodec.BytesToString(bytes.Repeat([]byte{0x11}, 20))
	require.NoError(t, err)
	moderatorAddress, err := addressCodec.BytesToString(bytes.Repeat([]byte{0x22}, 20))
	require.NoError(t, err)
	constitutionGenesis.BaseAddress = baseAddress
	constitutionGenesis.ModeratorAddress = moderatorAddress
	genesis[constitutiontypes.ModuleName] = seedApp.AppCodec().MustMarshalJSON(constitutionGenesis)
	configureExportTestValidator(t, seedApp, genesis)
	appState, err := json.Marshal(genesis)
	require.NoError(t, err)

	initTime := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	_, err = seedApp.InitChain(&abci.RequestInitChain{
		ChainId:         chainID,
		InitialHeight:   1,
		Time:            initTime,
		AppStateBytes:   appState,
		ConsensusParams: simtestutil.DefaultConsensusParams,
	})
	require.NoError(t, err)
	_, err = seedApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   initTime.Add(time.Second),
	})
	require.NoError(t, err)
	_, err = seedApp.Commit()
	require.NoError(t, err)
	require.NoError(t, seedApp.Close())

	trackingDB := &closeTrackingDB{DB: db}
	exported, err := appExport(logger, trackingDB, nil, -1, false, nil, appOpts, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), exported.Height)
	require.NotEmpty(t, exported.AppState)
	require.NotNil(t, exported.ConsensusParams)
	require.Equal(t, int32(1), trackingDB.closeCount.Load())
}

func TestAppExportClosesApplicationWhenLoadHeightFails(t *testing.T) {
	const chainID = "guru-export-close-test-1"
	configureExportTestBech32Prefixes(t)

	appOpts := viper.New()
	appOpts.Set(flags.FlagHome, writeChainIDGenesis(t, chainID))
	appOpts.Set(flags.FlagChainID, chainID)
	appOpts.Set("oracle.enabled", false)
	appOpts.Set(sdkserver.FlagMempoolMaxTxs, -1)
	trackingDB := &closeTrackingDB{DB: dbm.NewMemDB()}

	_, err := appExport(log.NewNopLogger(), trackingDB, nil, 99, false, nil, appOpts, nil)
	require.Error(t, err)
	require.Equal(t, int32(1), trackingDB.closeCount.Load())
}

type closeTrackingDB struct {
	dbm.DB
	closeCount atomic.Int32
}

func (db *closeTrackingDB) Close() error {
	db.closeCount.Add(1)
	return db.DB.Close()
}

func configureExportTestBech32Prefixes(t *testing.T) {
	t.Helper()
	cfg := sdk.GetConfig()
	if cfg.GetBech32AccountAddrPrefix() == appparams.Bech32PrefixAccAddr &&
		cfg.GetBech32ValidatorAddrPrefix() == appparams.Bech32PrefixValAddr &&
		cfg.GetBech32ConsensusAddrPrefix() == appparams.Bech32PrefixConsAddr {
		return
	}
	accountAddr := cfg.GetBech32AccountAddrPrefix()
	accountPub := cfg.GetBech32AccountPubPrefix()
	validatorAddr := cfg.GetBech32ValidatorAddrPrefix()
	validatorPub := cfg.GetBech32ValidatorPubPrefix()
	consensusAddr := cfg.GetBech32ConsensusAddrPrefix()
	consensusPub := cfg.GetBech32ConsensusPubPrefix()

	cfg.SetBech32PrefixForAccount(appparams.Bech32PrefixAccAddr, appparams.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(appparams.Bech32PrefixValAddr, appparams.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(appparams.Bech32PrefixConsAddr, appparams.Bech32PrefixConsPub)
	t.Cleanup(func() {
		cfg.SetBech32PrefixForAccount(accountAddr, accountPub)
		cfg.SetBech32PrefixForValidator(validatorAddr, validatorPub)
		cfg.SetBech32PrefixForConsensusNode(consensusAddr, consensusPub)
	})
}

func configureExportTestValidator(t *testing.T, app *guruapp.App, genesis map[string]json.RawMessage) {
	t.Helper()

	bond := sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction)
	pubKey := simtestutil.CreateTestPubKeys(1)[0]
	validatorBytes := sdk.ValAddress(pubKey.Address().Bytes())
	validatorAddress, err := app.StakingKeeper.ValidatorAddressCodec().BytesToString(validatorBytes)
	require.NoError(t, err)
	validator, err := stakingtypes.NewValidator(
		validatorAddress,
		pubKey,
		stakingtypes.Description{Moniker: "export-validator"},
	)
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	validator.Tokens = bond
	validator.DelegatorShares = sdkmath.LegacyOneDec()

	delegatorAddress, err := app.AccountKeeper.AddressCodec().BytesToString(sdk.AccAddress(validatorBytes))
	require.NoError(t, err)
	stakingGenesis := stakingtypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	stakingGenesis.Validators = []stakingtypes.Validator{validator}
	stakingGenesis.Delegations = []stakingtypes.Delegation{
		stakingtypes.NewDelegation(delegatorAddress, validatorAddress, sdkmath.LegacyOneDec()),
	}
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)

	bondedPoolAddress, err := app.AccountKeeper.AddressCodec().BytesToString(
		authtypes.NewModuleAddress(stakingtypes.BondedPoolName),
	)
	require.NoError(t, err)
	bankGenesis := banktypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], bankGenesis)
	bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
		Address: bondedPoolAddress,
		Coins:   sdk.NewCoins(sdk.NewCoin(stakingGenesis.Params.BondDenom, bond)),
	})
	genesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)
}

func TestPatchExportCommandHidesUnsupportedZeroHeightFlags(t *testing.T) {
	rootCmd := &cobra.Command{Use: "gurud"}
	exportCmd := sdkserver.ExportCmd(nil, t.TempDir())
	rootCmd.AddCommand(exportCmd)

	patchExportCommand(rootCmd)

	require.True(t, exportCmd.Flags().Lookup(sdkserver.FlagForZeroHeight).Hidden)
	require.True(t, exportCmd.Flags().Lookup(sdkserver.FlagJailAllowedAddrs).Hidden)
	require.False(t, exportCmd.Flags().Lookup(sdkserver.FlagHeight).Hidden)
}

func TestPatchExportedGenesisBytesEnablesVoteExtensions(t *testing.T) {
	genesis := genutiltypes.AppGenesis{
		InitialHeight: 12,
		Consensus:     &genutiltypes.ConsensusGenesis{Params: cmttypes.DefaultConsensusParams()},
	}
	bz, err := json.Marshal(&genesis)
	require.NoError(t, err)

	patched, err := patchExportedGenesisBytes(bz)
	require.NoError(t, err)

	var out genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(patched, &out))
	require.NotNil(t, out.Consensus)
	require.NotNil(t, out.Consensus.Params)
	require.Equal(t, int64(12), out.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}

func TestPatchExportedGenesisBytesPreservesLaterVoteExtensionHeight(t *testing.T) {
	params := cmttypes.DefaultConsensusParams()
	params.ABCI.VoteExtensionsEnableHeight = 9
	genesis := genutiltypes.AppGenesis{
		InitialHeight: 5,
		Consensus:     &genutiltypes.ConsensusGenesis{Params: params},
	}
	bz, err := json.Marshal(&genesis)
	require.NoError(t, err)

	patched, err := patchExportedGenesisBytes(bz)
	require.NoError(t, err)

	var out genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(patched, &out))
	require.Equal(t, int64(9), out.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}
