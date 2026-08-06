package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

const (
	blockSTMCosmosFeeBenchmarkChainID = "guru-blockstm-cosmos-fee-benchmark-1"
	blockSTMCosmosFeeBenchmarkTxEnv   = "GURU_BLOCKSTM_BENCH_TXS"
	blockSTMCosmosFeeBenchmarkWorkEnv = "GURU_BLOCKSTM_BENCH_WORKERS"

	blockSTMCosmosFeeBenchmarkDefaultTxs = 128
	blockSTMCosmosFeeBenchmarkMaxTxs     = 20_000
	blockSTMCosmosFeeBenchmarkGas        = uint64(200_000)
	blockSTMCosmosFeeBenchmarkFee        = int64(200_000)
	blockSTMCosmosFeeBenchmarkFunds      = int64(1_000_000_000_000_000_000)
)

type blockSTMCosmosFeeBenchmarkWorkload uint8

const (
	blockSTMCosmosFeeBenchmarkIndependent blockSTMCosmosFeeBenchmarkWorkload = iota
	blockSTMCosmosFeeBenchmarkSameAccount
)

type blockSTMCosmosFeeBenchmarkAccount struct {
	priv          *secp256k1.PrivKey
	address       sdk.AccAddress
	addressString string
	accountNumber uint64
}

type blockSTMCosmosFeeBenchmarkFixture struct {
	app                 *App
	payers              []blockSTMCosmosFeeBenchmarkAccount
	recipients          []sdk.AccAddress
	nextHeight          int64
	startTime           time.Time
	independentSequence uint64
	sameAccountSequence uint64
}

// BenchmarkBlockSTMCosmosFeeCollection exercises the production BlockSTM,
// FeePolicy, signed Cosmos transaction, FinalizeBlock, and Commit paths. The
// exact same source file can be copied to an unpatched dev-v3 worktree to
// compare normal fee collection with virtual fee collection.
//
// GURU_BLOCKSTM_BENCH_TXS controls transactions per block (default 128).
// GURU_BLOCKSTM_BENCH_WORKERS controls BlockSTM workers (default min(10,
// GOMAXPROCS)). For stable comparisons, pin GOMAXPROCS and run with a fixed
// -benchtime count, for example:
//
//	GOMAXPROCS=10 GURU_BLOCKSTM_BENCH_TXS=1000 \
//	  go test ./app -run '^$' -bench '^BenchmarkBlockSTMCosmosFeeCollection$' \
//	  -benchmem -benchtime=10x -count=5
func BenchmarkBlockSTMCosmosFeeCollection(b *testing.B) {
	configureBlockSTMCosmosFeeBenchmarkPrefixes(b)

	txCount := blockSTMCosmosFeeBenchmarkEnvInt(
		b,
		blockSTMCosmosFeeBenchmarkTxEnv,
		blockSTMCosmosFeeBenchmarkDefaultTxs,
		1,
		blockSTMCosmosFeeBenchmarkMaxTxs,
	)
	defaultWorkers := runtime.GOMAXPROCS(0)
	if defaultWorkers > 10 {
		defaultWorkers = 10
	}
	workers := blockSTMCosmosFeeBenchmarkEnvInt(
		b,
		blockSTMCosmosFeeBenchmarkWorkEnv,
		defaultWorkers,
		1,
		1_024,
	)
	fixture := newBlockSTMCosmosFeeBenchmarkFixture(b, txCount, workers)

	for _, workload := range []struct {
		name string
		kind blockSTMCosmosFeeBenchmarkWorkload
	}{
		{name: "independent_payers", kind: blockSTMCosmosFeeBenchmarkIndependent},
		{name: "same_account", kind: blockSTMCosmosFeeBenchmarkSameAccount},
	} {
		b.Run(fmt.Sprintf("%s/txs_%d/workers_%d", workload.name, txCount, workers), func(b *testing.B) {
			blockSTMCosmosFeeBenchmarkRun(b, fixture, workload.kind, txCount)
		})
	}
}

func blockSTMCosmosFeeBenchmarkRun(
	b *testing.B,
	fixture *blockSTMCosmosFeeBenchmarkFixture,
	workload blockSTMCosmosFeeBenchmarkWorkload,
	txCount int,
) {
	b.Helper()

	blocks := make([][][]byte, b.N)
	for blockIndex := 0; blockIndex < b.N; blockIndex++ {
		blocks[blockIndex] = fixture.signedBlock(b, workload, txCount, blockIndex)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ReportAllocs()
	b.ResetTimer()

	var finalizeDuration, commitDuration time.Duration
	for blockIndex := 0; blockIndex < b.N; blockIndex++ {
		finalizeStarted := time.Now()
		response, err := fixture.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: fixture.nextHeight,
			Time:   fixture.startTime.Add(time.Duration(fixture.nextHeight) * time.Second),
			Txs:    blocks[blockIndex],
		})
		finalizeDuration += time.Since(finalizeStarted)
		if err != nil {
			b.Fatalf("FinalizeBlock at height %d: %v", fixture.nextHeight, err)
		}
		if len(response.TxResults) != txCount {
			b.Fatalf("FinalizeBlock returned %d results, want %d", len(response.TxResults), txCount)
		}
		for txIndex, result := range response.TxResults {
			if result.Code != abci.CodeTypeOK {
				b.Fatalf("tx %d at height %d failed with code %d: %s", txIndex, fixture.nextHeight, result.Code, result.Log)
			}
		}

		commitStarted := time.Now()
		if _, err := fixture.app.Commit(); err != nil {
			b.Fatalf("Commit at height %d: %v", fixture.nextHeight, err)
		}
		commitDuration += time.Since(commitStarted)
		fixture.nextHeight++
	}

	b.StopTimer()
	runtime.ReadMemStats(&after)
	totalTxs := float64(b.N * txCount)
	totalDuration := finalizeDuration + commitDuration
	b.ReportMetric(float64(totalDuration.Nanoseconds())/totalTxs, "ns/tx")
	b.ReportMetric(float64(finalizeDuration.Nanoseconds())/totalTxs, "finalize_ns/tx")
	b.ReportMetric(float64(commitDuration.Nanoseconds())/totalTxs, "commit_ns/tx")
	b.ReportMetric(totalTxs/totalDuration.Seconds(), "tx/s")
	b.ReportMetric(float64(after.Mallocs-before.Mallocs)/totalTxs, "allocs/tx")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/totalTxs, "B/tx")
	if workload == blockSTMCosmosFeeBenchmarkIndependent {
		fixture.independentSequence += uint64(b.N)
	} else {
		fixture.sameAccountSequence += uint64(b.N * txCount)
	}
}

func newBlockSTMCosmosFeeBenchmarkFixture(
	b *testing.B,
	txCount int,
	workers int,
) *blockSTMCosmosFeeBenchmarkFixture {
	b.Helper()

	// One extra payer is reserved for the same-account workload. Keeping both
	// workloads in one App avoids relying on Cosmos EVM's test-only global reset
	// hooks and also keeps the benchmark command valid without custom build tags.
	payerCount := txCount + 1
	payers := make([]blockSTMCosmosFeeBenchmarkAccount, payerCount)
	for i := range payers {
		payers[i] = newBlockSTMCosmosFeeBenchmarkAccount(b, "payer", i, uint64(i))
	}
	recipients := make([]sdk.AccAddress, txCount)
	for i := range recipients {
		recipients[i] = newBlockSTMCosmosFeeBenchmarkAccount(b, "recipient", i, 0).address
	}

	options := simtestutil.AppOptionsMap{
		"oracle.enabled":               false,
		server.FlagMempoolMaxTxs:       -1,
		server.FlagBlockExecutor:       serverconfig.BlockExecutorBlockSTM,
		server.FlagBlockSTMWorkers:     workers,
		server.FlagBlockSTMPreEstimate: true,
	}
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		options,
		baseapp.SetChainID(blockSTMCosmosFeeBenchmarkChainID),
	)
	b.Cleanup(func() {
		if err := testApp.Close(); err != nil {
			b.Errorf("close benchmark app: %v", err)
		}
	})

	fixture := &blockSTMCosmosFeeBenchmarkFixture{
		app:        testApp,
		payers:     payers,
		recipients: recipients,
		nextHeight: 2,
		startTime:  time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC),
	}
	genesis := testApp.BuildChainDefaultGenesis()
	fixture.setConstitutionGenesis(b, genesis)
	fixture.setAuthGenesis(b, genesis)
	fixture.setStakingGenesis(b, genesis)
	fixture.setBankGenesis(b, genesis)
	fixture.setFeeMarketGenesis(b, genesis)
	fixture.setMintGenesis(b, genesis)
	fixture.setFeePolicyGenesis(b, genesis)
	if err := testApp.ValidateChainGenesis(genesis); err != nil {
		b.Fatalf("validate benchmark genesis: %v", err)
	}

	appState, err := json.Marshal(genesis)
	if err != nil {
		b.Fatalf("marshal benchmark genesis: %v", err)
	}
	if _, err := testApp.InitChain(&abci.RequestInitChain{
		ChainId:         blockSTMCosmosFeeBenchmarkChainID,
		InitialHeight:   1,
		Time:            fixture.startTime,
		AppStateBytes:   appState,
		ConsensusParams: simtestutil.DefaultConsensusParams,
	}); err != nil {
		b.Fatalf("InitChain benchmark app: %v", err)
	}
	initialBlock, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   fixture.startTime.Add(time.Second),
	})
	if err != nil {
		b.Fatalf("initial FinalizeBlock: %v", err)
	}
	if len(initialBlock.TxResults) != 0 {
		b.Fatalf("initial FinalizeBlock returned %d tx results", len(initialBlock.TxResults))
	}
	if _, err := testApp.Commit(); err != nil {
		b.Fatalf("initial Commit: %v", err)
	}

	return fixture
}

func newBlockSTMCosmosFeeBenchmarkAccount(
	b *testing.B,
	role string,
	index int,
	accountNumber uint64,
) blockSTMCosmosFeeBenchmarkAccount {
	b.Helper()

	priv := secp256k1.GenPrivKeyFromSecret([]byte(fmt.Sprintf(
		"%s/%s/%d",
		blockSTMCosmosFeeBenchmarkChainID,
		role,
		index,
	)))
	address := sdk.AccAddress(priv.PubKey().Address())
	addressString, err := sdk.Bech32ifyAddressBytes(appparams.Bech32PrefixAccAddr, address)
	if err != nil {
		b.Fatalf("encode %s %d address: %v", role, index, err)
	}
	return blockSTMCosmosFeeBenchmarkAccount{
		priv:          priv,
		address:       address,
		addressString: addressString,
		accountNumber: accountNumber,
	}
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) setConstitutionGenesis(
	b *testing.B,
	genesis map[string]json.RawMessage,
) {
	b.Helper()

	state := &constitutiontypes.GenesisState{}
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], state)
	state.BaseAddress = fixture.payers[0].addressString
	state.ModeratorAddress = fixture.payers[0].addressString
	genesis[constitutiontypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) setAuthGenesis(
	b *testing.B,
	genesis map[string]json.RawMessage,
) {
	b.Helper()

	state := authtypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[authtypes.ModuleName], state)
	accounts := make(authtypes.GenesisAccounts, len(fixture.payers))
	for i, payer := range fixture.payers {
		accounts[i] = authtypes.NewBaseAccount(
			payer.address,
			payer.priv.PubKey(),
			payer.accountNumber,
			0,
		)
	}
	state = authtypes.NewGenesisState(state.Params, accounts)
	genesis[authtypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) setStakingGenesis(
	b *testing.B,
	genesis map[string]json.RawMessage,
) {
	b.Helper()

	bond := sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction)
	pubKey := simtestutil.CreateTestPubKeys(1)[0]
	validatorBytes := sdk.ValAddress(pubKey.Address().Bytes())
	validatorAddress, err := fixture.app.StakingKeeper.ValidatorAddressCodec().BytesToString(validatorBytes)
	if err != nil {
		b.Fatalf("encode benchmark validator address: %v", err)
	}
	validator, err := stakingtypes.NewValidator(
		validatorAddress,
		pubKey,
		stakingtypes.Description{Moniker: "blockstm-cosmos-fee-benchmark-validator"},
	)
	if err != nil {
		b.Fatalf("create benchmark validator: %v", err)
	}
	validator.Status = stakingtypes.Bonded
	validator.Tokens = bond
	validator.DelegatorShares = sdkmath.LegacyNewDecFromInt(bond)

	delegatorAddress, err := fixture.app.AccountKeeper.AddressCodec().BytesToString(
		sdk.AccAddress(validatorBytes),
	)
	if err != nil {
		b.Fatalf("encode benchmark delegator address: %v", err)
	}
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

func (fixture *blockSTMCosmosFeeBenchmarkFixture) setBankGenesis(
	b *testing.B,
	genesis map[string]json.RawMessage,
) {
	b.Helper()

	state := banktypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], state)
	state.Balances = make([]banktypes.Balance, 0, len(fixture.payers)+1)
	for _, payer := range fixture.payers {
		state.Balances = append(state.Balances, banktypes.Balance{
			Address: payer.addressString,
			Coins: sdk.NewCoins(sdk.NewInt64Coin(
				appparams.BaseDenom,
				blockSTMCosmosFeeBenchmarkFunds,
			)),
		})
	}
	bondedPoolAddress, err := fixture.app.AccountKeeper.AddressCodec().BytesToString(
		authtypes.NewModuleAddress(stakingtypes.BondedPoolName),
	)
	if err != nil {
		b.Fatalf("encode bonded pool address: %v", err)
	}
	state.Balances = append(state.Balances, banktypes.Balance{
		Address: bondedPoolAddress,
		Coins: sdk.NewCoins(sdk.NewCoin(
			appparams.BaseDenom,
			sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction),
		)),
	})
	state.Supply = nil
	genesis[banktypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) setMintGenesis(
	b *testing.B,
	genesis map[string]json.RawMessage,
) {
	b.Helper()

	state := minttypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[minttypes.ModuleName], state)
	state.Minter = minttypes.InitialMinter(sdkmath.LegacyZeroDec())
	state.Params.InflationRateChange = sdkmath.LegacyZeroDec()
	state.Params.InflationMax = sdkmath.LegacyZeroDec()
	state.Params.InflationMin = sdkmath.LegacyZeroDec()
	genesis[minttypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) setFeeMarketGenesis(
	b *testing.B,
	genesis map[string]json.RawMessage,
) {
	b.Helper()

	state := feemarkettypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[feemarkettypes.ModuleName], state)
	state.Params.NoBaseFee = true
	state.Params.BaseFee = sdkmath.LegacyZeroDec()
	state.Params.MinGasPrice = sdkmath.LegacyOneDec()
	genesis[feemarkettypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) setFeePolicyGenesis(
	b *testing.B,
	genesis map[string]json.RawMessage,
) {
	b.Helper()

	state := &feepolicytypes.GenesisState{
		ModeratorAddress: fixture.payers[0].addressString,
		Discounts: []feepolicytypes.AccountDiscount{
			feePolicyTestBankSendDiscount(
				"",
				feepolicytypes.FeeDiscountTypeFixed,
				sdkmath.LegacyNewDec(12_345),
			),
		},
	}
	genesis[feepolicytypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(state)
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) signedBlock(
	b *testing.B,
	workload blockSTMCosmosFeeBenchmarkWorkload,
	txCount int,
	blockIndex int,
) [][]byte {
	b.Helper()

	txs := make([][]byte, txCount)
	for txIndex := 0; txIndex < txCount; txIndex++ {
		payerIndex := txIndex
		sequence := fixture.independentSequence + uint64(blockIndex)
		if workload == blockSTMCosmosFeeBenchmarkSameAccount {
			payerIndex = len(fixture.payers) - 1
			sequence = fixture.sameAccountSequence + uint64(blockIndex*txCount+txIndex)
		}
		txs[txIndex] = fixture.signTx(
			b,
			fixture.payers[payerIndex],
			fixture.recipients[txIndex],
			sequence,
		)
	}
	return txs
}

func (fixture *blockSTMCosmosFeeBenchmarkFixture) signTx(
	b *testing.B,
	signer blockSTMCosmosFeeBenchmarkAccount,
	recipient sdk.AccAddress,
	sequence uint64,
) []byte {
	b.Helper()

	builder := fixture.app.TxConfig().NewTxBuilder()
	msg := banktypes.NewMsgSend(
		signer.address,
		recipient,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
	)
	if err := builder.SetMsgs(msg); err != nil {
		b.Fatalf("set MsgSend: %v", err)
	}
	builder.SetGasLimit(blockSTMCosmosFeeBenchmarkGas)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(
		appparams.BaseDenom,
		blockSTMCosmosFeeBenchmarkFee,
	)))

	signMode, err := authsigning.APISignModeToInternal(
		fixture.app.TxConfig().SignModeHandler().DefaultMode(),
	)
	if err != nil {
		b.Fatalf("resolve sign mode: %v", err)
	}
	signature := signing.SignatureV2{
		PubKey: signer.priv.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode: signMode,
		},
		Sequence: sequence,
	}
	if err := builder.SetSignatures(signature); err != nil {
		b.Fatalf("set placeholder signature: %v", err)
	}

	signerData := authsigning.SignerData{
		Address:       signer.addressString,
		ChainID:       blockSTMCosmosFeeBenchmarkChainID,
		AccountNumber: signer.accountNumber,
		Sequence:      sequence,
		PubKey:        signer.priv.PubKey(),
	}
	signBytes, err := authsigning.GetSignBytesAdapter(
		context.Background(),
		fixture.app.TxConfig().SignModeHandler(),
		signMode,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		b.Fatalf("build sign bytes: %v", err)
	}
	rawSignature, err := signer.priv.Sign(signBytes)
	if err != nil {
		b.Fatalf("sign transaction: %v", err)
	}
	signature.Data.(*signing.SingleSignatureData).Signature = rawSignature
	if err := builder.SetSignatures(signature); err != nil {
		b.Fatalf("set final signature: %v", err)
	}

	txBytes, err := fixture.app.TxConfig().TxEncoder()(builder.GetTx())
	if err != nil {
		b.Fatalf("encode transaction: %v", err)
	}
	return txBytes
}

func blockSTMCosmosFeeBenchmarkEnvInt(
	b *testing.B,
	name string,
	defaultValue int,
	minimum int,
	maximum int,
) int {
	b.Helper()

	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		b.Fatalf("%s must be an integer in [%d, %d], got %q", name, minimum, maximum, raw)
	}
	return value
}

func configureBlockSTMCosmosFeeBenchmarkPrefixes(b *testing.B) {
	b.Helper()

	config := sdk.GetConfig()
	accountAddress := config.GetBech32AccountAddrPrefix()
	accountPubKey := config.GetBech32AccountPubPrefix()
	validatorAddress := config.GetBech32ValidatorAddrPrefix()
	validatorPubKey := config.GetBech32ValidatorPubPrefix()
	consensusAddress := config.GetBech32ConsensusAddrPrefix()
	consensusPubKey := config.GetBech32ConsensusPubPrefix()

	config.SetBech32PrefixForAccount(appparams.Bech32PrefixAccAddr, appparams.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(appparams.Bech32PrefixValAddr, appparams.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(appparams.Bech32PrefixConsAddr, appparams.Bech32PrefixConsPub)
	b.Cleanup(func() {
		config.SetBech32PrefixForAccount(accountAddress, accountPubKey)
		config.SetBech32PrefixForValidator(validatorAddress, validatorPubKey)
		config.SetBech32PrefixForConsensusNode(consensusAddress, consensusPubKey)
	})
}
