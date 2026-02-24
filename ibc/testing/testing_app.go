package ibctesting

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	dbm "github.com/cosmos/cosmos-db"
	ibctesting "github.com/cosmos/ibc-go/v10/testing"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/gurufinglobal/guru/v2/cmd/gurud/config"
	"github.com/gurufinglobal/guru/v2/gurud"
	"github.com/gurufinglobal/guru/v2/ibc/simapp"
	feemarkettypes "github.com/gurufinglobal/guru/v2/x/feemarket/types"
	evmtypes "github.com/gurufinglobal/guru/v2/x/vm/types"
)

// evmDenomMetadata returns the bank denom metadata required for InitEvmCoinInfo.
func evmDenomMetadata() banktypes.Metadata {
	coinInfo := config.ChainsCoinInfo[config.EighteenDecimalsChainID]
	return banktypes.Metadata{
		Description: "The native EVM token of the chain",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: coinInfo.Denom, Exponent: 0},
			{Denom: coinInfo.DisplayDenom, Exponent: coinInfo.Decimals},
		},
		Base:    coinInfo.Denom,
		Display: coinInfo.DisplayDenom,
		Name:    coinInfo.DisplayDenom,
		Symbol:  coinInfo.DisplayDenom,
	}
}

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

	// Ensure EVM genesis has ActiveStaticPrecompiles and Preinstalls
	var evmGenesis evmtypes.GenesisState
	if err := app.AppCodec().UnmarshalJSON(genesisState[evmtypes.ModuleName], &evmGenesis); err == nil {
		if len(evmGenesis.Params.ActiveStaticPrecompiles) == 0 {
			evmGenesis.Params.ActiveStaticPrecompiles = evmtypes.AvailableStaticPrecompiles
		}
		if len(evmGenesis.Preinstalls) == 0 {
			evmGenesis.Preinstalls = evmtypes.DefaultPreinstalls()
		}
		genesisState[evmtypes.ModuleName] = app.AppCodec().MustMarshalJSON(&evmGenesis)
	}

	return app, genesisState
}

func SetupTestingApp() (ibctesting.TestingApp, map[string]json.RawMessage) {
	db := dbm.NewMemDB()
	app := simapp.NewSimApp(log.NewNopLogger(), db, nil, true, simtestutil.EmptyAppOptions{})
	return app, app.DefaultGenesis()
}

// SetupWithGenesisValSet is a local version of ibctesting.SetupWithGenesisValSet
// that includes EVM denom metadata in the bank genesis state. The upstream ibc-go
// version creates a bank genesis with empty metadata, which causes InitEvmCoinInfo
// to panic because it cannot find denom metadata for the EVM denom.
func SetupWithGenesisValSet(tb testing.TB, valSet *cmttypes.ValidatorSet, genAccs []authtypes.GenesisAccount, chainID string, powerReduction sdkmath.Int, balances ...banktypes.Balance) ibctesting.TestingApp {
	tb.Helper()
	app, genesisState := ibctesting.DefaultTestingAppInit()

	// ensure baseapp has a chain-id set before running InitChain
	baseapp.SetChainID(chainID)(app.GetBaseApp())

	// set genesis accounts
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), genAccs)
	genesisState[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenesis)

	validators := make([]stakingtypes.Validator, 0, len(valSet.Validators))
	delegations := make([]stakingtypes.Delegation, 0, len(valSet.Validators))

	bondAmt := sdk.TokensFromConsensusPower(1, powerReduction)

	for _, val := range valSet.Validators {
		pk, err := cryptocodec.FromCmtPubKeyInterface(val.PubKey)
		require.NoError(tb, err)
		pkAny, err := types.NewAnyWithValue(pk)
		require.NoError(tb, err)
		validator := stakingtypes.Validator{
			OperatorAddress:   sdk.ValAddress(val.Address).String(),
			ConsensusPubkey:   pkAny,
			Jailed:            false,
			Status:            stakingtypes.Bonded,
			Tokens:            bondAmt,
			DelegatorShares:   sdkmath.LegacyOneDec(),
			Description:       stakingtypes.Description{},
			UnbondingHeight:   int64(0),
			UnbondingTime:     time.Unix(0, 0).UTC(),
			Commission:        stakingtypes.NewCommission(sdkmath.LegacyZeroDec(), sdkmath.LegacyZeroDec(), sdkmath.LegacyZeroDec()),
			MinSelfDelegation: sdkmath.ZeroInt(),
		}

		validators = append(validators, validator)
		delegations = append(delegations, stakingtypes.NewDelegation(genAccs[0].GetAddress().String(), sdk.ValAddress(val.Address).String(), sdkmath.LegacyOneDec()))
	}

	// set validators and delegations
	var stakingGenesis stakingtypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesisState[stakingtypes.ModuleName], &stakingGenesis)

	bondDenom := stakingGenesis.Params.BondDenom

	// add bonded amount to bonded pool module account
	balances = append(balances, banktypes.Balance{
		Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(),
		Coins:   sdk.Coins{sdk.NewCoin(bondDenom, bondAmt.Mul(sdkmath.NewInt(int64(len(valSet.Validators)))))},
	})

	// set validators and delegations
	stakingGenesis = *stakingtypes.NewGenesisState(stakingGenesis.Params, validators, delegations)
	genesisState[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(&stakingGenesis)

	// update total supply — include EVM denom metadata (required by InitEvmCoinInfo)
	bankGenesis := banktypes.NewGenesisState(
		banktypes.DefaultGenesisState().Params,
		balances,
		sdk.NewCoins(),
		[]banktypes.Metadata{evmDenomMetadata()},
		[]banktypes.SendEnabled{},
	)
	genesisState[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(tb, err)

	// init chain will set the validator set and initialize the genesis accounts
	_, err = app.InitChain(
		&abci.RequestInitChain{
			ChainId:         chainID,
			Validators:      []abci.ValidatorUpdate{},
			AppStateBytes:   stateBytes,
			ConsensusParams: simtestutil.DefaultConsensusParams,
		},
	)
	require.NoError(tb, err)

	return app
}
