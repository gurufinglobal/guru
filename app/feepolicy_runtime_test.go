package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	antetypes "github.com/cosmos/evm/ante/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
	"github.com/stretchr/testify/require"
)

const (
	feePolicyRuntimeChainID      = "guru-feepolicy-runtime-1"
	feePolicyRuntimeInitialFunds = int64(2_000_000)
	feePolicyRuntimeLowFunds     = int64(100_000)
	feePolicyRuntimeGas          = uint64(200_000)
	feePolicyRuntimeDeclaredFee  = int64(200_000)
	feePolicyRuntimeWorkerEnv    = "GURU_FEEPOLICY_RUNTIME_WORKER"
	feePolicyRuntimeTipWorkerEnv = "GURU_FEEPOLICY_RUNTIME_TIP_WORKER"

	feePolicyActorNormal           = "normal"
	feePolicyActorDynamic          = "dynamic"
	feePolicyActorGrantPayer       = "grant-payer"
	feePolicyActorGrantGranter     = "grant-granter"
	feePolicyActorGrantBankPayer   = "grant-bank-failure-payer"
	feePolicyActorGrantBankGranter = "grant-bank-failure-granter"
	feePolicyActorGrantSigPayer    = "grant-signature-failure-payer"
	feePolicyActorGrantSigGranter  = "grant-signature-failure-granter"
	feePolicyActorGrantMsgPayer    = "grant-message-failure-payer"
	feePolicyActorGrantMsgGranter  = "grant-message-failure-granter"
	feePolicyActorDenyPayer        = "deny-payer"
	feePolicyActorDenyGranter      = "deny-granter"
	feePolicyActorSelfGranter      = "self-granter"
	feePolicyActorBoundary         = "boundary"
	feePolicyActorNominal          = "nominal-admission"
	feePolicyActorZero             = "zero-actual"
	feePolicyActorRecipient        = "recipient"
)

var feePolicyRuntimeActorNames = []string{
	feePolicyActorNormal,
	feePolicyActorDynamic,
	feePolicyActorGrantPayer,
	feePolicyActorGrantGranter,
	feePolicyActorGrantBankPayer,
	feePolicyActorGrantBankGranter,
	feePolicyActorGrantSigPayer,
	feePolicyActorGrantSigGranter,
	feePolicyActorGrantMsgPayer,
	feePolicyActorGrantMsgGranter,
	feePolicyActorDenyPayer,
	feePolicyActorDenyGranter,
	feePolicyActorSelfGranter,
	feePolicyActorBoundary,
	feePolicyActorNominal,
	feePolicyActorZero,
	feePolicyActorRecipient,
}

type feePolicyRuntimeSigner struct {
	priv          *secp256k1.PrivKey
	address       sdk.AccAddress
	addressString string
	accountNumber uint64
}

type feePolicyRuntimeFixture struct {
	app       *App
	actors    map[string]feePolicyRuntimeSigner
	nextBlock int64
	startTime time.Time
}

type feePolicyRuntimeTxOptions struct {
	gasLimit   uint64
	feeGranter sdk.AccAddress
	extension  *antetypes.ExtensionOptionDynamicFeeTx
	corruptSig bool
}

func TestFeePolicyRuntimeSignedCosmosTransactions(t *testing.T) {
	if !runFeePolicyRuntimeWorker(t, feePolicyRuntimeWorkerEnv) {
		return
	}

	fixture := newFeePolicyRuntimeFixture(t, feePolicyRuntimeGenesisOptions{
		noBaseFee:   true,
		baseFee:     sdkmath.LegacyZeroDec(),
		minGasPrice: sdkmath.LegacyOneDec(),
	})
	recipient := fixture.actor(feePolicyActorRecipient)

	t.Run("normal cosmos account percent policy accounting", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorNormal)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)

		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(7))
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{}))

		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		requireIntEqual(t, beforePayer.SubRaw(150_007), fixture.balance(payer.address))
		requireIntEqual(t, beforeRecipient.AddRaw(7), fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.NewInt(150_000), fixture.collectorBalance())
		require.Equal(t, uint64(1), fixture.sequence(payer.address))
		fixture.requireFeeEvent(t, result, 150_000, payer.addressString)
	})

	t.Run("granter policy funds actual fee and consumes actual allowance", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorGrantPayer)
		granter := fixture.actor(feePolicyActorGrantGranter)
		beforePayer := fixture.balance(payer.address)
		beforeGranter := fixture.balance(granter.address)
		beforeRecipient := fixture.balance(recipient.address)

		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{
			feeGranter: granter.address,
		}))

		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		requireIntEqual(t, beforePayer.SubRaw(1), fixture.balance(payer.address))
		requireIntEqual(t, beforeGranter.SubRaw(150_000), fixture.balance(granter.address))
		requireIntEqual(t, beforeRecipient.AddRaw(1), fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.NewInt(150_000), fixture.collectorBalance())
		requireIntEqual(t, sdkmath.NewInt(250_000), fixture.allowance(t, granter, payer))
		require.Equal(t, uint64(1), fixture.sequence(payer.address))
		fixture.requireFeeEvent(t, result, 150_000, granter.addressString)
	})

	t.Run("insufficient allowance rolls back funding and sequence", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorDenyPayer)
		granter := fixture.actor(feePolicyActorDenyGranter)
		beforePayer := fixture.balance(payer.address)
		beforeGranter := fixture.balance(granter.address)
		beforeRecipient := fixture.balance(recipient.address)

		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{
			feeGranter: granter.address,
		}))

		require.NotEqual(t, abci.CodeTypeOK, result.Code)
		requireIntEqual(t, beforePayer, fixture.balance(payer.address))
		requireIntEqual(t, beforeGranter, fixture.balance(granter.address))
		requireIntEqual(t, beforeRecipient, fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.ZeroInt(), fixture.collectorBalance())
		requireIntEqual(t, sdkmath.NewInt(100_000), fixture.allowance(t, granter, payer))
		require.Equal(t, uint64(0), fixture.sequence(payer.address))
		fixture.requireNoFeeEvent(t, result)
	})

	t.Run("bank debit failure rolls back sufficient feegrant allowance", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorGrantBankPayer)
		granter := fixture.actor(feePolicyActorGrantBankGranter)
		beforePayer := fixture.balance(payer.address)
		beforeGranter := fixture.balance(granter.address)
		beforeRecipient := fixture.balance(recipient.address)
		beforeAllowance := fixture.allowance(t, granter, payer)
		beforePayerSequence := fixture.sequence(payer.address)
		beforeGranterSequence := fixture.sequence(granter.address)

		requireIntEqual(t, sdkmath.NewInt(400_000), beforeAllowance)
		requireIntEqual(t, sdkmath.NewInt(feePolicyRuntimeLowFunds), beforeGranter)
		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{
			feeGranter: granter.address,
		}))

		require.NotEqual(t, abci.CodeTypeOK, result.Code)
		require.Contains(t, result.Log, "insufficient funds")
		requireIntEqual(t, beforePayer, fixture.balance(payer.address))
		requireIntEqual(t, beforeGranter, fixture.balance(granter.address))
		requireIntEqual(t, beforeRecipient, fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.ZeroInt(), fixture.collectorBalance())
		requireIntEqual(t, beforeAllowance, fixture.allowance(t, granter, payer))
		require.Equal(t, beforePayerSequence, fixture.sequence(payer.address))
		require.Equal(t, beforeGranterSequence, fixture.sequence(granter.address))
		fixture.requireNoFeeEvent(t, result)
	})

	t.Run("payer equal to granter does not require an allowance", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorSelfGranter)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)

		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{
			feeGranter: payer.address,
		}))

		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		requireIntEqual(t, beforePayer.SubRaw(150_001), fixture.balance(payer.address))
		requireIntEqual(t, beforeRecipient.AddRaw(1), fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.NewInt(150_000), fixture.collectorBalance())
		require.Equal(t, uint64(1), fixture.sequence(payer.address))
		fixture.requireFeeEvent(t, result, 150_000, payer.addressString)
	})

	t.Run("simulation check recheck do not commit and finalize charges once", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorBoundary)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)
		beforeCollector := fixture.collectorBalance()
		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		txBytes := fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{})

		_, simulationResult, err := fixture.app.Simulate(txBytes)
		require.NoError(t, err)
		require.NotNil(t, simulationResult)
		fixture.requireCommittedState(t, payer, beforePayer, beforeRecipient, beforeCollector, 0)

		checkResult, err := fixture.app.CheckTx(&abci.RequestCheckTx{Tx: txBytes, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, checkResult.Code, checkResult.Log)
		fixture.requireCommittedState(t, payer, beforePayer, beforeRecipient, beforeCollector, 0)

		fixture.finalizeEmpty(t)
		fixture.requireCommittedState(t, payer, beforePayer, beforeRecipient, sdkmath.ZeroInt(), 0)

		recheckResult, err := fixture.app.CheckTx(&abci.RequestCheckTx{Tx: txBytes, Type: abci.CheckTxType_Recheck})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, recheckResult.Code, recheckResult.Log)
		fixture.requireCommittedState(t, payer, beforePayer, beforeRecipient, sdkmath.ZeroInt(), 0)

		result := fixture.finalize(t, txBytes)
		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		fixture.requireCommittedState(
			t,
			payer,
			beforePayer.SubRaw(150_001),
			beforeRecipient.AddRaw(1),
			sdkmath.NewInt(150_000),
			1,
		)
		fixture.requireFeeEvent(t, result, 150_000, payer.addressString)
	})

	t.Run("feegrant late signature failure rolls back granter funding allowance and sequences", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorGrantSigPayer)
		granter := fixture.actor(feePolicyActorGrantSigGranter)
		beforePayer := fixture.balance(payer.address)
		beforeGranter := fixture.balance(granter.address)
		beforeRecipient := fixture.balance(recipient.address)
		beforeAllowance := fixture.allowance(t, granter, payer)
		beforePayerSequence := fixture.sequence(payer.address)
		beforeGranterSequence := fixture.sequence(granter.address)

		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{
			feeGranter: granter.address,
			corruptSig: true,
		}))

		require.NotEqual(t, abci.CodeTypeOK, result.Code)
		require.Contains(t, result.Log, "signature verification failed")
		requireIntEqual(t, beforePayer, fixture.balance(payer.address))
		requireIntEqual(t, beforeGranter, fixture.balance(granter.address))
		requireIntEqual(t, beforeRecipient, fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.ZeroInt(), fixture.collectorBalance())
		requireIntEqual(t, beforeAllowance, fixture.allowance(t, granter, payer))
		require.Equal(t, beforePayerSequence, fixture.sequence(payer.address))
		require.Equal(t, beforeGranterSequence, fixture.sequence(granter.address))
		fixture.requireNoFeeEvent(t, result)
	})

	t.Run("feegrant message failure commits actual granter fee allowance and payer sequence only", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorGrantMsgPayer)
		granter := fixture.actor(feePolicyActorGrantMsgGranter)
		beforePayer := fixture.balance(payer.address)
		beforeGranter := fixture.balance(granter.address)
		beforeRecipient := fixture.balance(recipient.address)
		beforeAllowance := fixture.allowance(t, granter, payer)
		beforePayerSequence := fixture.sequence(payer.address)
		beforeGranterSequence := fixture.sequence(granter.address)

		msg := banktypes.NewMsgSend(
			payer.address,
			recipient.address,
			feePolicyRuntimeCoins(feePolicyRuntimeInitialFunds+1),
		)
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{
			feeGranter: granter.address,
		}))

		require.NotEqual(t, abci.CodeTypeOK, result.Code)
		require.Contains(t, result.Log, "insufficient funds")
		requireIntEqual(t, beforePayer, fixture.balance(payer.address))
		requireIntEqual(t, beforeGranter.SubRaw(150_000), fixture.balance(granter.address))
		requireIntEqual(t, beforeRecipient, fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.NewInt(150_000), fixture.collectorBalance())
		requireIntEqual(t, beforeAllowance.SubRaw(150_000), fixture.allowance(t, granter, payer))
		require.Equal(t, beforePayerSequence+1, fixture.sequence(payer.address))
		require.Equal(t, beforeGranterSequence, fixture.sequence(granter.address))
		fixture.requireFeeEvent(t, result, 150_000, granter.addressString)
	})

	t.Run("nominal fee admission happens before a full discount", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorNominal)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)
		beforeCollector := fixture.collectorBalance()
		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		txBytes := fixture.signTxWithFee(
			t,
			payer,
			[]sdk.Msg{msg},
			feePolicyRuntimeCoins(feePolicyRuntimeDeclaredFee-1),
			feePolicyRuntimeTxOptions{},
		)

		result, err := fixture.app.CheckTx(&abci.RequestCheckTx{Tx: txBytes, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.NotEqual(t, abci.CodeTypeOK, result.Code)
		require.Contains(t, result.Log, "minimum global fee")
		fixture.requireCommittedState(t, payer, beforePayer, beforeRecipient, beforeCollector, 0)
	})

	t.Run("full discount records a zero actual fee without moving funds", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorZero)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)
		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{}))

		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		fixture.requireCommittedState(
			t,
			payer,
			beforePayer.SubRaw(1),
			beforeRecipient.AddRaw(1),
			sdkmath.ZeroInt(),
			1,
		)
		fixture.requireFeeEvent(t, result, 0, payer.addressString)
	})

}

func TestFeePolicyRuntimeDynamicTipIsNotDiscounted(t *testing.T) {
	if !runFeePolicyRuntimeWorker(t, feePolicyRuntimeTipWorkerEnv) {
		return
	}

	fixture := newFeePolicyRuntimeFixture(t, feePolicyRuntimeGenesisOptions{
		noBaseFee:   false,
		baseFee:     sdkmath.LegacyNewDecWithPrec(4, 1),
		minGasPrice: sdkmath.LegacyZeroDec(),
	})
	feeMarketParams := fixture.app.FeeMarketKeeper.GetParams(fixture.committedContext())
	require.False(t, feeMarketParams.NoBaseFee)
	require.True(t, feeMarketParams.BaseFee.Equal(sdkmath.LegacyNewDecWithPrec(4, 1)))
	payer := fixture.actor(feePolicyActorDynamic)
	recipient := fixture.actor(feePolicyActorRecipient)
	beforePayer := fixture.balance(payer.address)
	beforeRecipient := fixture.balance(recipient.address)

	msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
	result := fixture.finalize(t, fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{
		extension: &antetypes.ExtensionOptionDynamicFeeTx{
			MaxPriorityPrice: sdkmath.LegacyNewDecWithPrec(4, 1),
		},
	}))

	// The standardized 21,000-gas projection applies before fee accounting.
	// With a 0.4/gas priority cap and fixed global discount of 12,345, the
	// resulting charged fee is 15,750.
	const expectedActualFee = int64(15_750)
	require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
	fixture.requireCommittedState(
		t,
		payer,
		beforePayer.SubRaw(expectedActualFee+1),
		beforeRecipient.AddRaw(1),
		sdkmath.NewInt(expectedActualFee),
		1,
	)
	fixture.requireFeeEvent(t, result, expectedActualFee, payer.addressString)
}

type feePolicyRuntimeGenesisOptions struct {
	noBaseFee   bool
	baseFee     sdkmath.LegacyDec
	minGasPrice sdkmath.LegacyDec
}

func runFeePolicyRuntimeWorker(t *testing.T, workerEnv string) bool {
	t.Helper()
	if os.Getenv(workerEnv) == "1" {
		return true
	}

	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
	cmd.Env = append(os.Environ(), workerEnv+"=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "feepolicy runtime subprocess failed:\n%s", output)
	return false
}

func newFeePolicyRuntimeFixture(
	t *testing.T,
	genesisOptions feePolicyRuntimeGenesisOptions,
) *feePolicyRuntimeFixture {
	t.Helper()
	configureFeePolicyTestBech32Prefixes(t, false)

	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		feePolicyTestAppOptions(),
		baseapp.SetChainID(feePolicyRuntimeChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	fixture := &feePolicyRuntimeFixture{
		app:       testApp,
		actors:    make(map[string]feePolicyRuntimeSigner, len(feePolicyRuntimeActorNames)),
		nextBlock: 2,
		startTime: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
	}
	for accountNumber, name := range feePolicyRuntimeActorNames {
		fixture.actors[name] = newFeePolicyRuntimeSigner(t, testApp, name, uint64(accountNumber))
	}

	genesis := testApp.BuildChainDefaultGenesis()
	setFeePolicyRuntimeConstitutionGenesis(t, fixture, genesis)
	setFeePolicyRuntimeAuthGenesis(t, fixture, genesis)
	setFeePolicyRuntimeStakingGenesis(t, fixture, genesis)
	setFeePolicyRuntimeBankGenesis(t, fixture, genesis)
	setFeePolicyRuntimeFeeMarketGenesis(t, fixture, genesis)
	setFeePolicyRuntimeMintGenesis(t, fixture, genesis)
	setFeePolicyRuntimePolicyGenesis(t, fixture, genesis)
	setFeePolicyRuntimeFeeGrantGenesis(t, fixture, genesis)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	appState, err := json.Marshal(genesis)
	require.NoError(t, err)
	_, err = testApp.InitChain(&abci.RequestInitChain{
		ChainId:         feePolicyRuntimeChainID,
		InitialHeight:   1,
		Time:            fixture.startTime,
		AppStateBytes:   appState,
		ConsensusParams: simtestutil.DefaultConsensusParams,
	})
	require.NoError(t, err)

	initialBlock, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   fixture.startTime.Add(time.Second),
	})
	require.NoError(t, err)
	require.Empty(t, initialBlock.TxResults)
	_, err = testApp.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), testApp.LastBlockHeight())

	if !genesisOptions.noBaseFee || !genesisOptions.baseFee.IsZero() ||
		!genesisOptions.minGasPrice.Equal(sdkmath.LegacyOneDec()) {
		ctx := testApp.NewNextBlockContext(cmtproto.Header{
			ChainID: feePolicyRuntimeChainID,
			Height:  2,
			Time:    fixture.startTime.Add(2 * time.Second),
		})
		params := testApp.FeeMarketKeeper.GetParams(ctx)
		params.NoBaseFee = genesisOptions.noBaseFee
		params.BaseFee = genesisOptions.baseFee
		params.MinGasPrice = genesisOptions.minGasPrice
		require.NoError(t, testApp.FeeMarketKeeper.SetParams(ctx, params))
		testApp.SimWriteState()
		_, err = testApp.Commit()
		require.NoError(t, err)
		fixture.nextBlock = testApp.LastBlockHeight() + 1
	}

	return fixture
}

func newFeePolicyRuntimeSigner(
	t *testing.T,
	app *App,
	name string,
	accountNumber uint64,
) feePolicyRuntimeSigner {
	t.Helper()

	priv := secp256k1.GenPrivKeyFromSecret([]byte(fmt.Sprintf("%s/%s", t.Name(), name)))
	address := sdk.AccAddress(priv.PubKey().Address())
	addressString, err := app.AccountKeeper.AddressCodec().BytesToString(address)
	require.NoError(t, err)

	return feePolicyRuntimeSigner{
		priv:          priv,
		address:       address,
		addressString: addressString,
		accountNumber: accountNumber,
	}
}

func setFeePolicyRuntimeConstitutionGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	state := &constitutiontypes.GenesisState{}
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], state)
	state.BaseAddress = fixture.actor(feePolicyActorRecipient).addressString
	state.ModeratorAddress = fixture.actor(feePolicyActorNormal).addressString
	genesis[constitutiontypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func setFeePolicyRuntimeAuthGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	state := authtypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[authtypes.ModuleName], state)
	accounts := make(authtypes.GenesisAccounts, 0, len(feePolicyRuntimeActorNames))
	for _, name := range feePolicyRuntimeActorNames {
		signer := fixture.actor(name)
		accounts = append(accounts, authtypes.NewBaseAccount(
			signer.address,
			signer.priv.PubKey(),
			signer.accountNumber,
			0,
		))
	}
	state = authtypes.NewGenesisState(state.Params, accounts)
	genesis[authtypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func setFeePolicyRuntimeStakingGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	bond := sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction)
	pubKey := simtestutil.CreateTestPubKeys(1)[0]
	validatorBytes := sdk.ValAddress(pubKey.Address().Bytes())
	validatorAddress, err := fixture.app.StakingKeeper.ValidatorAddressCodec().BytesToString(validatorBytes)
	require.NoError(t, err)
	validator, err := stakingtypes.NewValidator(
		validatorAddress,
		pubKey,
		stakingtypes.Description{Moniker: "feepolicy-runtime-validator"},
	)
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	validator.Tokens = bond
	validator.DelegatorShares = sdkmath.LegacyNewDecFromInt(bond)

	delegatorAddress, err := fixture.app.AccountKeeper.AddressCodec().BytesToString(sdk.AccAddress(validatorBytes))
	require.NoError(t, err)
	state := stakingtypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], state)
	state.Validators = []stakingtypes.Validator{validator}
	state.Delegations = []stakingtypes.Delegation{
		stakingtypes.NewDelegation(
			delegatorAddress,
			validatorAddress,
			sdkmath.LegacyNewDecFromInt(bond),
		),
	}
	genesis[stakingtypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func setFeePolicyRuntimeBankGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	state := banktypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], state)
	state.Balances = make([]banktypes.Balance, 0, len(feePolicyRuntimeActorNames))
	for _, name := range feePolicyRuntimeActorNames {
		if name == feePolicyActorRecipient {
			continue
		}
		signer := fixture.actor(name)
		amount := feePolicyRuntimeInitialFunds
		if name == feePolicyActorGrantBankGranter {
			amount = feePolicyRuntimeLowFunds
		}
		state.Balances = append(state.Balances, banktypes.Balance{
			Address: signer.addressString,
			Coins:   feePolicyRuntimeCoins(amount),
		})
	}

	bond := sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction)
	bondedPoolAddress, err := fixture.app.AccountKeeper.AddressCodec().BytesToString(
		authtypes.NewModuleAddress(stakingtypes.BondedPoolName),
	)
	require.NoError(t, err)
	state.Balances = append(state.Balances, banktypes.Balance{
		Address: bondedPoolAddress,
		Coins:   sdk.NewCoins(sdk.NewCoin(appparams.BaseDenom, bond)),
	})
	state.Supply = nil
	genesis[banktypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func setFeePolicyRuntimeFeeMarketGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	state := feemarkettypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[feemarkettypes.ModuleName], state)
	// These values are target-wide genesis invariants. The dynamic-tip worker
	// mutates the committed params with BaseApp's test context after InitChain so
	// it can exercise a non-zero EIP-1559 base/tip split without constructing an
	// invalid application genesis.
	state.Params.NoBaseFee = true
	state.Params.BaseFee = sdkmath.LegacyZeroDec()
	state.Params.MinGasPrice = sdkmath.LegacyOneDec()
	genesis[feemarkettypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func setFeePolicyRuntimeMintGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	state := minttypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[minttypes.ModuleName], state)
	state.Minter = minttypes.InitialMinter(sdkmath.LegacyZeroDec())
	state.Params.InflationRateChange = sdkmath.LegacyZeroDec()
	state.Params.InflationMax = sdkmath.LegacyZeroDec()
	state.Params.InflationMin = sdkmath.LegacyZeroDec()
	genesis[minttypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func setFeePolicyRuntimePolicyGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	state := &feepolicytypes.GenesisState{
		ModeratorAddress: fixture.actor(feePolicyActorNormal).addressString,
		Discounts: []feepolicytypes.AccountDiscount{
			feePolicyTestBankSendDiscount("", feepolicytypes.FeeDiscountTypeFixed, sdkmath.LegacyNewDec(12_345)),
		},
	}
	for _, name := range []string{
		feePolicyActorNormal,
		feePolicyActorGrantGranter,
		feePolicyActorGrantBankGranter,
		feePolicyActorGrantSigGranter,
		feePolicyActorGrantMsgGranter,
		feePolicyActorDenyGranter,
		feePolicyActorSelfGranter,
		feePolicyActorBoundary,
	} {
		state.Discounts = append(state.Discounts, feePolicyTestBankSendDiscount(
			fixture.actor(name).addressString,
			feepolicytypes.FeeDiscountTypePercent,
			sdkmath.LegacyNewDec(25),
		))
	}
	state.Discounts = append(state.Discounts, feePolicyTestBankSendDiscount(
		fixture.actor(feePolicyActorNominal).addressString,
		feepolicytypes.FeeDiscountTypePercent,
		sdkmath.LegacyNewDec(100),
	))
	state.Discounts = append(state.Discounts, feePolicyTestBankSendDiscount(
		fixture.actor(feePolicyActorZero).addressString,
		feepolicytypes.FeeDiscountTypePercent,
		sdkmath.LegacyNewDec(100),
	))
	genesis[feepolicytypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func setFeePolicyRuntimeFeeGrantGenesis(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	specs := []struct {
		payer, granter string
		spendLimit     int64
	}{
		{feePolicyActorGrantPayer, feePolicyActorGrantGranter, 400_000},
		{feePolicyActorGrantBankPayer, feePolicyActorGrantBankGranter, 400_000},
		{feePolicyActorGrantSigPayer, feePolicyActorGrantSigGranter, 400_000},
		{feePolicyActorGrantMsgPayer, feePolicyActorGrantMsgGranter, 400_000},
		{feePolicyActorDenyPayer, feePolicyActorDenyGranter, 100_000},
	}
	grants := make([]feegrant.Grant, 0, len(specs))
	for _, spec := range specs {
		grant, err := feegrant.NewGrant(
			fixture.actor(spec.granter).address,
			fixture.actor(spec.payer).address,
			&feegrant.BasicAllowance{SpendLimit: feePolicyRuntimeCoins(spec.spendLimit)},
		)
		require.NoError(t, err)
		grants = append(grants, grant)
	}
	genesis[feegrant.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(feegrant.NewGenesisState(grants))
}

func (fixture *feePolicyRuntimeFixture) actor(name string) feePolicyRuntimeSigner {
	signer, ok := fixture.actors[name]
	if !ok {
		panic(fmt.Sprintf("unknown feepolicy runtime actor %q", name))
	}
	return signer
}

func (fixture *feePolicyRuntimeFixture) signTx(
	t *testing.T,
	signer feePolicyRuntimeSigner,
	msgs []sdk.Msg,
	options feePolicyRuntimeTxOptions,
) []byte {
	t.Helper()
	return fixture.signTxWithFee(t, signer, msgs, feePolicyRuntimeCoins(feePolicyRuntimeDeclaredFee), options)
}

func (fixture *feePolicyRuntimeFixture) signTxWithFee(
	t *testing.T,
	signer feePolicyRuntimeSigner,
	msgs []sdk.Msg,
	fee sdk.Coins,
	options feePolicyRuntimeTxOptions,
) []byte {
	t.Helper()

	builder := fixture.app.TxConfig().NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msgs...))
	gasLimit := feePolicyRuntimeGas
	if options.gasLimit > 0 {
		gasLimit = options.gasLimit
	}
	builder.SetGasLimit(gasLimit)
	builder.SetFeeAmount(fee)
	if options.feeGranter != nil {
		builder.SetFeeGranter(options.feeGranter)
	}
	if options.extension != nil {
		extension, err := codectypes.NewAnyWithValue(options.extension)
		require.NoError(t, err)
		extensionBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder)
		require.True(t, ok)
		extensionBuilder.SetExtensionOptions(extension)
	}

	signMode, err := authsigning.APISignModeToInternal(
		fixture.app.TxConfig().SignModeHandler().DefaultMode(),
	)
	require.NoError(t, err)
	signature := signing.SignatureV2{
		PubKey: signer.priv.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode: signMode,
		},
		Sequence: 0,
	}
	require.NoError(t, builder.SetSignatures(signature))

	signerData := authsigning.SignerData{
		Address:       signer.addressString,
		ChainID:       feePolicyRuntimeChainID,
		AccountNumber: signer.accountNumber,
		Sequence:      0,
		PubKey:        signer.priv.PubKey(),
	}
	signBytes, err := authsigning.GetSignBytesAdapter(
		context.Background(),
		fixture.app.TxConfig().SignModeHandler(),
		signMode,
		signerData,
		builder.GetTx(),
	)
	require.NoError(t, err)
	rawSignature, err := signer.priv.Sign(signBytes)
	require.NoError(t, err)
	require.NotEmpty(t, rawSignature)
	if options.corruptSig {
		rawSignature[0] ^= 0xff
	}
	signature.Data.(*signing.SingleSignatureData).Signature = rawSignature
	require.NoError(t, builder.SetSignatures(signature))

	txBytes, err := fixture.app.TxConfig().TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

func (fixture *feePolicyRuntimeFixture) finalize(t *testing.T, txBytes []byte) *abci.ExecTxResult {
	t.Helper()

	response, err := fixture.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: fixture.nextBlock,
		Time:   fixture.startTime.Add(time.Duration(fixture.nextBlock) * time.Second),
		Txs:    [][]byte{txBytes},
	})
	require.NoError(t, err)
	require.Len(t, response.TxResults, 1)
	_, err = fixture.app.Commit()
	require.NoError(t, err)
	fixture.nextBlock++
	return response.TxResults[0]
}

func (fixture *feePolicyRuntimeFixture) finalizeEmpty(t *testing.T) {
	t.Helper()

	response, err := fixture.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: fixture.nextBlock,
		Time:   fixture.startTime.Add(time.Duration(fixture.nextBlock) * time.Second),
	})
	require.NoError(t, err)
	require.Empty(t, response.TxResults)
	_, err = fixture.app.Commit()
	require.NoError(t, err)
	fixture.nextBlock++
}

func (fixture *feePolicyRuntimeFixture) committedContext() sdk.Context {
	return fixture.app.NewNextBlockContext(cmtproto.Header{
		ChainID: feePolicyRuntimeChainID,
		Height:  fixture.app.LastBlockHeight(),
		Time:    fixture.startTime.Add(time.Duration(fixture.app.LastBlockHeight()) * time.Second),
	})
}

func (fixture *feePolicyRuntimeFixture) balance(address sdk.AccAddress) sdkmath.Int {
	return fixture.app.BankKeeper.GetBalance(
		fixture.committedContext(),
		address,
		appparams.BaseDenom,
	).Amount
}

func (fixture *feePolicyRuntimeFixture) collectorBalance() sdkmath.Int {
	return fixture.balance(fixture.app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName))
}

func (fixture *feePolicyRuntimeFixture) sequence(address sdk.AccAddress) uint64 {
	account := fixture.app.AccountKeeper.GetAccount(fixture.committedContext(), address)
	if account == nil {
		return 0
	}
	return account.GetSequence()
}

func (fixture *feePolicyRuntimeFixture) allowance(
	t *testing.T,
	granter feePolicyRuntimeSigner,
	payer feePolicyRuntimeSigner,
) sdkmath.Int {
	t.Helper()

	allowance, err := fixture.app.FeeGrantKeeper.GetAllowance(
		fixture.committedContext(),
		granter.address,
		payer.address,
	)
	require.NoError(t, err)
	basicAllowance, ok := allowance.(*feegrant.BasicAllowance)
	require.True(t, ok, "unexpected allowance type %T", allowance)
	return basicAllowance.SpendLimit.AmountOf(appparams.BaseDenom)
}

func (fixture *feePolicyRuntimeFixture) requireCommittedState(
	t *testing.T,
	payer feePolicyRuntimeSigner,
	expectedPayer sdkmath.Int,
	expectedRecipient sdkmath.Int,
	expectedCollector sdkmath.Int,
	expectedSequence uint64,
) {
	t.Helper()
	requireIntEqual(t, expectedPayer, fixture.balance(payer.address))
	requireIntEqual(t, expectedRecipient, fixture.balance(fixture.actor(feePolicyActorRecipient).address))
	requireIntEqual(t, expectedCollector, fixture.collectorBalance())
	require.Equal(t, expectedSequence, fixture.sequence(payer.address))
}

func (fixture *feePolicyRuntimeFixture) requireFeeEvent(
	t *testing.T,
	result *abci.ExecTxResult,
	expectedFee int64,
	expectedPayer string,
) {
	t.Helper()

	event, found := feePolicyTestFeeEventFromResult(result)
	require.True(t, found, "fee event missing from %+v", result.Events)
	expectedFeeString := sdk.Coins{}.String()
	if expectedFee != 0 {
		expectedFeeString = feePolicyRuntimeCoins(expectedFee).String()
	}
	require.Equal(t, expectedFeeString, event.Fee)
	require.Equal(t, expectedPayer, event.Payer)
}

func (fixture *feePolicyRuntimeFixture) requireNoFeeEvent(t *testing.T, result *abci.ExecTxResult) {
	t.Helper()
	_, found := feePolicyTestFeeEventFromResult(result)
	require.False(t, found, "unexpected fee event in %+v", result.Events)
}

func feePolicyRuntimeCoins(amount int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, amount))
}

func requireIntEqual(t *testing.T, expected, actual sdkmath.Int) {
	t.Helper()
	require.True(t, expected.Equal(actual), "expected %s, got %s", expected, actual)
}

func TestFeePolicyAllMsgAndQueryServicesExecuteThroughRuntimeRouters(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	_, hasModuleAccountPermissions := appparams.DefaultModuleAccountPermissions()[feepolicytypes.ModuleName]
	require.False(t, hasModuleAccountPermissions)
	require.Nil(t, testApp.AccountKeeper.GetModuleAddress(feepolicytypes.ModuleName))

	ctx := testApp.NewNextBlockContext(cmtproto.Header{
		ChainID: appparams.SDKChainID,
		Height:  1,
		Time:    time.Unix(1_700_000_000, 0).UTC(),
	})
	moderator, err := testApp.AccountKeeper.AddressCodec().BytesToString(feePolicyServiceAddressBytes(0xa9))
	require.NoError(t, err)
	newModerator, err := testApp.AccountKeeper.AddressCodec().BytesToString(feePolicyServiceAddressBytes(0xaa))
	require.NoError(t, err)
	require.NoError(t, testApp.ConstitutionKeeper.SetModeratorAddress(ctx, moderator))

	account, err := testApp.AccountKeeper.AddressCodec().BytesToString(feePolicyServiceAddressBytes(0xab))
	require.NoError(t, err)
	removedAccount, err := testApp.AccountKeeper.AddressCodec().BytesToString(feePolicyServiceAddressBytes(0xac))
	require.NoError(t, err)
	expectedPolicy := feePolicyServicePolicy(account)

	executedMsgs := map[string]struct{}{}
	executeFeePolicyServiceMsg(
		t,
		testApp,
		ctx,
		"RegisterDiscounts",
		&feepolicytypes.MsgRegisterDiscounts{
			ModeratorAddress: moderator,
			Discounts: []feepolicytypes.AccountDiscount{
				expectedPolicy,
				feePolicyServicePolicy(removedAccount),
			},
		},
		&feepolicytypes.MsgRegisterDiscountsResponse{},
		executedMsgs,
	)
	executeFeePolicyServiceMsg(
		t,
		testApp,
		ctx,
		"RemoveDiscounts",
		&feepolicytypes.MsgRemoveDiscounts{
			ModeratorAddress: moderator,
			Address:          removedAccount,
		},
		&feepolicytypes.MsgRemoveDiscountsResponse{},
		executedMsgs,
	)
	executeFeePolicyServiceMsg(
		t,
		testApp,
		ctx,
		"ChangeModerator",
		&feepolicytypes.MsgChangeModerator{
			ModeratorAddress:    moderator,
			NewModeratorAddress: newModerator,
		},
		&feepolicytypes.MsgChangeModeratorResponse{},
		executedMsgs,
	)
	requireFeePolicyServiceMethodsExecuted(t, reflect.TypeOf((*feepolicytypes.MsgServer)(nil)).Elem(), executedMsgs)

	constitutionModerator, err := testApp.ConstitutionKeeper.GetModeratorAddress(ctx)
	require.NoError(t, err)
	require.Equal(t, newModerator, constitutionModerator)

	executedQueries := map[string]struct{}{}
	moderatorResponse := &feepolicytypes.QueryModeratorAddressResponse{}
	executeFeePolicyServiceQuery(
		t,
		testApp,
		ctx,
		"ModeratorAddress",
		&feepolicytypes.QueryModeratorAddressRequest{},
		moderatorResponse,
		executedQueries,
	)
	require.Equal(t, newModerator, moderatorResponse.GetModeratorAddress())

	discountsResponse := &feepolicytypes.QueryDiscountsResponse{}
	executeFeePolicyServiceQuery(
		t,
		testApp,
		ctx,
		"Discounts",
		&feepolicytypes.QueryDiscountsRequest{},
		discountsResponse,
		executedQueries,
	)
	require.Equal(t, []feepolicytypes.AccountDiscount{expectedPolicy}, discountsResponse.GetDiscounts())

	discountResponse := &feepolicytypes.QueryDiscountResponse{}
	executeFeePolicyServiceQuery(
		t,
		testApp,
		ctx,
		"Discount",
		&feepolicytypes.QueryDiscountRequest{Address: account},
		discountResponse,
		executedQueries,
	)
	require.Equal(t, expectedPolicy, discountResponse.GetDiscount())
	requireFeePolicyServiceMethodsExecuted(t, reflect.TypeOf((*feepolicytypes.QueryServer)(nil)).Elem(), executedQueries)
}

func executeFeePolicyServiceMsg(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request sdk.Msg,
	response gogoRuntimeMessage,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "FeePolicy Msg RPC executed more than once")
	handler := testApp.MsgServiceRouter().Handler(request)
	require.NotNil(t, handler, "FeePolicy Msg RPC %s is not registered", method)
	result, err := handler(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.MsgResponses, 1)
	require.Equal(t, "/guru.feepolicy.v1.Msg"+method+"Response", result.MsgResponses[0].TypeUrl)
	require.NoError(t, response.Unmarshal(result.Data))
	executed[method] = struct{}{}
	t.Logf("runtime Msg/%s => %v", method, response)
}

func executeFeePolicyServiceQuery(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request gogoRuntimeMessage,
	response gogoRuntimeMessage,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "FeePolicy Query RPC executed more than once")
	handler := testApp.GRPCQueryRouter().Route("/guru.feepolicy.v1.Query/" + method)
	require.NotNil(t, handler, "FeePolicy Query RPC %s is not registered", method)
	requestBytes, err := request.Marshal()
	require.NoError(t, err)
	queryResult, err := handler(ctx, &abci.RequestQuery{Data: requestBytes, Height: ctx.BlockHeight()})
	require.NoError(t, err)
	require.NotNil(t, queryResult)
	require.Equal(t, ctx.BlockHeight(), queryResult.Height)
	require.NoError(t, response.Unmarshal(queryResult.Value))
	executed[method] = struct{}{}
	t.Logf("runtime Query/%s => %v", method, response)
}

func requireFeePolicyServiceMethodsExecuted(t *testing.T, service reflect.Type, executed map[string]struct{}) {
	t.Helper()
	require.Len(t, executed, service.NumMethod())
	for i := 0; i < service.NumMethod(); i++ {
		require.Contains(t, executed, service.Method(i).Name, "runtime RPC coverage must track the generated FeePolicy service interface")
	}
}

func feePolicyServicePolicy(address string) feepolicytypes.AccountDiscount {
	return feePolicyTestBankSendDiscount(
		address,
		feepolicytypes.FeeDiscountTypePercent,
		sdkmath.LegacyMustNewDecFromStr("12.5"),
	)
}

func feePolicyServiceAddressBytes(value byte) []byte {
	address := make([]byte, 20)
	for i := range address {
		address[i] = value
	}
	return address
}

type feePolicyTestFeeEvent struct {
	Fee   string
	Payer string
}

func feePolicyTestAppOptions() simtestutil.AppOptionsMap {
	return simtestutil.AppOptionsMap{
		"oracle.enabled":         false,
		server.FlagMempoolMaxTxs: -1,
	}
}

func feePolicyTestBankSendDiscount(
	address string,
	discountType string,
	amount sdkmath.LegacyDec,
) feepolicytypes.AccountDiscount {
	return feepolicytypes.AccountDiscount{
		Address: address,
		Modules: []feepolicytypes.ModuleDiscount{
			{
				Module: banktypes.ModuleName,
				Discounts: []feepolicytypes.Discount{
					{
						DiscountType: discountType,
						MsgType:      sdk.MsgTypeURL(&banktypes.MsgSend{}),
						Amount:       amount,
					},
				},
			},
		},
	}
}

func feePolicyTestFeeEventFromResult(result *abci.ExecTxResult) (feePolicyTestFeeEvent, bool) {
	for _, event := range result.Events {
		if event.Type != sdk.EventTypeTx {
			continue
		}

		var parsed feePolicyTestFeeEvent
		var hasFee bool
		for _, attribute := range event.Attributes {
			switch attribute.Key {
			case sdk.AttributeKeyFee:
				parsed.Fee = attribute.Value
				hasFee = true
			case sdk.AttributeKeyFeePayer:
				parsed.Payer = attribute.Value
			}
		}
		if hasFee {
			return parsed, true
		}
	}
	return feePolicyTestFeeEvent{}, false
}

func configureFeePolicyTestBech32Prefixes(t *testing.T, disableAddressCache bool) {
	t.Helper()

	config := sdk.GetConfig()
	if !disableAddressCache &&
		config.GetBech32AccountAddrPrefix() == appparams.Bech32PrefixAccAddr &&
		config.GetBech32ValidatorAddrPrefix() == appparams.Bech32PrefixValAddr &&
		config.GetBech32ConsensusAddrPrefix() == appparams.Bech32PrefixConsAddr {
		return
	}

	addressCacheEnabled := sdk.IsAddrCacheEnabled()
	accountAddress := config.GetBech32AccountAddrPrefix()
	accountPubKey := config.GetBech32AccountPubPrefix()
	validatorAddress := config.GetBech32ValidatorAddrPrefix()
	validatorPubKey := config.GetBech32ValidatorPubPrefix()
	consensusAddress := config.GetBech32ConsensusAddrPrefix()
	consensusPubKey := config.GetBech32ConsensusPubPrefix()

	if disableAddressCache {
		sdk.SetAddrCacheEnabled(false)
	}
	config.SetBech32PrefixForAccount(appparams.Bech32PrefixAccAddr, appparams.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(appparams.Bech32PrefixValAddr, appparams.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(appparams.Bech32PrefixConsAddr, appparams.Bech32PrefixConsPub)
	t.Cleanup(func() {
		config.SetBech32PrefixForAccount(accountAddress, accountPubKey)
		config.SetBech32PrefixForValidator(validatorAddress, validatorPubKey)
		config.SetBech32PrefixForConsensusNode(consensusAddress, consensusPubKey)
		if disableAddressCache {
			sdk.SetAddrCacheEnabled(addressCacheEnabled)
		}
	})
}
