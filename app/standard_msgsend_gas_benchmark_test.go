package app

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	appante "github.com/gurufinglobal/guru/v3/app/ante"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

const (
	standardMsgSendGasBenchmarkTxEnv     = "GURU_STANDARD_MSGSEND_BENCH_TXS"
	standardMsgSendGasBenchmarkWorkerEnv = "GURU_STANDARD_MSGSEND_BENCH_WORKERS"

	standardMsgSendGasBenchmarkBlockGas     = 30_000_000
	standardMsgSendGasBenchmarkDefaultTxs   = 32
	standardMsgSendGasBenchmarkDefaultCPU   = 4
	standardMsgSendGasBenchmarkAdmissionTxs = int(
		standardMsgSendGasBenchmarkBlockGas / appante.StandardMsgSendGas,
	)
	standardMsgSendGasBenchmarkStressTxs = int(
		standardMsgSendGasBenchmarkBlockGas / appante.StandardMsgSendGas,
	)
	standardMsgSendGasBenchmarkFee = int64(appante.StandardMsgSendGas)
)

type standardMsgSendGasBenchmarkTopology uint8

const (
	standardMsgSendGasBenchmarkIndependent standardMsgSendGasBenchmarkTopology = iota
	standardMsgSendGasBenchmarkSharedRecipient
)

type standardMsgSendGasBenchmarkFixture struct {
	base                  *blockSTMCosmosFeeBenchmarkFixture
	signers               []blockSTMCosmosFeeBenchmarkAccount
	independentRecipients []sdk.AccAddress
	sharedRecipient       sdk.AccAddress
	nextSequence          uint64
}

// BenchmarkStandardMsgSendGasBlockSTM measures the production signed Cosmos
// transaction -> BlockSTM FinalizeBlock -> Commit path for the bounded 21k
// MsgSend class. Its genesis contains no payer-specific or global FeePolicy
// record, and every transaction is a single-denom MsgSend with exactly 21k
// declared gas and no feegranter, memo, or extension option.
//
// The two topologies reuse the same independently signed payers. All recipients
// are pre-created at genesis so the only intended topology difference is the
// bank recipient key: one distinct key per transaction versus one shared key.
// Production proposal admission charges the standardized 21k per candidate, so a
// 30M-gas block admits at most 1,428 of these transactions. A 1,428-transaction
// run is
// deliberately retained only as a direct-FinalizeBlock red-team workload that
// bypasses PrepareProposal/ProcessProposal; it is not a consensus-valid merge
// gate for the implemented admission contract. The smaller default keeps
// ordinary benchmark discovery inexpensive.
//
// Run the merge-gate measurement with a four-CPU affinity and a fixed iteration
// count, for example (through the repository's required remote test runner):
//
//	/Users/kimhyunwoo/.codex/bin/idc-test -- taskset -c 0-3 env \
//	  GOMAXPROCS=4 GURU_STANDARD_MSGSEND_BENCH_TXS=100 \
//	  GURU_STANDARD_MSGSEND_BENCH_WORKERS=4 \
//	  go test ./app -run '^$' -bench '^BenchmarkStandardMsgSendGasBlockSTM$' \
//	  -benchmem -benchtime=1x -count=5
func BenchmarkStandardMsgSendGasBlockSTM(b *testing.B) {
	configureBlockSTMCosmosFeeBenchmarkPrefixes(b)

	txCount := blockSTMCosmosFeeBenchmarkEnvInt(
		b,
		standardMsgSendGasBenchmarkTxEnv,
		standardMsgSendGasBenchmarkDefaultTxs,
		1,
		standardMsgSendGasBenchmarkStressTxs,
	)
	workers := blockSTMCosmosFeeBenchmarkEnvInt(
		b,
		standardMsgSendGasBenchmarkWorkerEnv,
		standardMsgSendGasBenchmarkDefaultCPU,
		1,
		standardMsgSendGasBenchmarkStressTxs,
	)
	if workers > runtime.GOMAXPROCS(0) {
		b.Fatalf(
			"%s=%d exceeds GOMAXPROCS=%d; pin at least as many execution slots as BlockSTM workers",
			standardMsgSendGasBenchmarkWorkerEnv,
			workers,
			runtime.GOMAXPROCS(0),
		)
	}

	fixture := newStandardMsgSendGasBenchmarkFixture(b, txCount, workers)
	for _, topology := range []struct {
		name string
		kind standardMsgSendGasBenchmarkTopology
	}{
		{name: "independent_recipients", kind: standardMsgSendGasBenchmarkIndependent},
		{name: "shared_recipient", kind: standardMsgSendGasBenchmarkSharedRecipient},
	} {
		b.Run(fmt.Sprintf("%s/txs_%d/workers_%d", topology.name, txCount, workers), func(b *testing.B) {
			standardMsgSendGasBenchmarkRun(b, fixture, topology.kind, txCount)
		})
	}
}

func standardMsgSendGasBenchmarkRun(
	b *testing.B,
	fixture *standardMsgSendGasBenchmarkFixture,
	topology standardMsgSendGasBenchmarkTopology,
	txCount int,
) {
	b.Helper()

	blocks := make([][][]byte, b.N)
	var encodedBytes uint64
	for blockIndex := 0; blockIndex < b.N; blockIndex++ {
		blocks[blockIndex] = fixture.signedBlock(b, topology, txCount, blockIndex)
		for _, txBytes := range blocks[blockIndex] {
			encodedBytes += uint64(len(txBytes))
		}
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ReportAllocs()
	b.ResetTimer()

	var finalizeDuration, commitDuration time.Duration
	for blockIndex := 0; blockIndex < b.N; blockIndex++ {
		finalizeStarted := time.Now()
		response, err := fixture.base.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: fixture.base.nextHeight,
			Time:   fixture.base.startTime.Add(time.Duration(fixture.base.nextHeight) * time.Second),
			Txs:    blocks[blockIndex],
		})
		finalizeDuration += time.Since(finalizeStarted)
		if err != nil {
			b.Fatalf("FinalizeBlock at height %d: %v", fixture.base.nextHeight, err)
		}
		if len(response.TxResults) != txCount {
			b.Fatalf("FinalizeBlock returned %d results, want %d", len(response.TxResults), txCount)
		}
		for txIndex, result := range response.TxResults {
			if result.Code != abci.CodeTypeOK {
				b.Fatalf(
					"tx %d at height %d failed with code %d: %s",
					txIndex,
					fixture.base.nextHeight,
					result.Code,
					result.Log,
				)
			}
			if result.GasWanted != standardMsgSendGasBenchmarkFee {
				b.Fatalf(
					"tx %d at height %d GasWanted=%d, want %d",
					txIndex,
					fixture.base.nextHeight,
					result.GasWanted,
					standardMsgSendGasBenchmarkFee,
				)
			}
			if result.GasUsed != standardMsgSendGasBenchmarkFee {
				b.Fatalf(
					"tx %d at height %d GasUsed=%d, want %d",
					txIndex,
					fixture.base.nextHeight,
					result.GasUsed,
					standardMsgSendGasBenchmarkFee,
				)
			}
		}

		commitStarted := time.Now()
		if _, err := fixture.base.app.Commit(); err != nil {
			b.Fatalf("Commit at height %d: %v", fixture.base.nextHeight, err)
		}
		commitDuration += time.Since(commitStarted)
		fixture.base.nextHeight++
	}

	b.StopTimer()
	runtime.ReadMemStats(&after)
	fixture.nextSequence += uint64(b.N)

	totalTxs := float64(b.N * txCount)
	totalDuration := finalizeDuration + commitDuration
	b.ReportMetric(float64(totalDuration.Nanoseconds())/totalTxs, "ns/tx")
	b.ReportMetric(float64(finalizeDuration.Nanoseconds())/totalTxs, "finalize_ns/tx")
	b.ReportMetric(float64(commitDuration.Nanoseconds())/totalTxs, "commit_ns/tx")
	b.ReportMetric(totalTxs/totalDuration.Seconds(), "tx/s")
	b.ReportMetric(float64(encodedBytes)/totalTxs, "bytes/tx")
	b.ReportMetric(float64(after.Mallocs-before.Mallocs)/totalTxs, "allocs/tx")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/totalTxs, "B/tx")
}

func newStandardMsgSendGasBenchmarkFixture(
	b *testing.B,
	txCount int,
	workers int,
) *standardMsgSendGasBenchmarkFixture {
	b.Helper()

	// Recipient auth accounts are initialized deliberately: this benchmark is
	// about BlockSTM key topology, not the one-time cost of account creation.
	allAccounts := make([]blockSTMCosmosFeeBenchmarkAccount, 0, 2*txCount+1)
	signers := make([]blockSTMCosmosFeeBenchmarkAccount, txCount)
	for i := range signers {
		signers[i] = newBlockSTMCosmosFeeBenchmarkAccount(b, "standard-msgsend-payer", i, uint64(len(allAccounts)))
		allAccounts = append(allAccounts, signers[i])
	}

	independentRecipients := make([]sdk.AccAddress, txCount)
	for i := range independentRecipients {
		recipient := newBlockSTMCosmosFeeBenchmarkAccount(
			b,
			"standard-msgsend-independent-recipient",
			i,
			uint64(len(allAccounts)),
		)
		allAccounts = append(allAccounts, recipient)
		independentRecipients[i] = recipient.address
	}
	sharedRecipientAccount := newBlockSTMCosmosFeeBenchmarkAccount(
		b,
		"standard-msgsend-shared-recipient",
		0,
		uint64(len(allAccounts)),
	)
	allAccounts = append(allAccounts, sharedRecipientAccount)

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
			b.Errorf("close standard MsgSend gas benchmark app: %v", err)
		}
	})

	baseFixture := &blockSTMCosmosFeeBenchmarkFixture{
		app:        testApp,
		payers:     allAccounts,
		nextHeight: 2,
		startTime:  time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
	}
	fixture := &standardMsgSendGasBenchmarkFixture{
		base:                  baseFixture,
		signers:               signers,
		independentRecipients: independentRecipients,
		sharedRecipient:       sharedRecipientAccount.address,
	}

	genesis := testApp.BuildChainDefaultGenesis()
	baseFixture.setConstitutionGenesis(b, genesis)
	baseFixture.setAuthGenesis(b, genesis)
	baseFixture.setStakingGenesis(b, genesis)
	baseFixture.setBankGenesis(b, genesis)
	baseFixture.setFeeMarketGenesis(b, genesis)
	baseFixture.setMintGenesis(b, genesis)
	// Explicitly keep both payer-specific and global FeePolicy records absent.
	// The standardized class bypasses policy lookup regardless, but an empty
	// genesis keeps the benchmark's state footprint unambiguous.
	genesis[feepolicytypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(
		feepolicytypes.DefaultGenesisState(),
	)
	if err := testApp.ValidateChainGenesis(genesis); err != nil {
		b.Fatalf("validate standard MsgSend gas benchmark genesis: %v", err)
	}

	appState, err := json.Marshal(genesis)
	if err != nil {
		b.Fatalf("marshal standard MsgSend gas benchmark genesis: %v", err)
	}
	consensusParams := *simtestutil.DefaultConsensusParams
	blockParams := *consensusParams.Block
	blockParams.MaxGas = int64(standardMsgSendGasBenchmarkBlockGas)
	// Keep transaction bytes from becoming the limiting dimension in this
	// gas-capacity benchmark. The measured bytes/tx metric remains visible.
	blockParams.MaxBytes = int64(standardMsgSendGasBenchmarkBlockGas)
	consensusParams.Block = &blockParams
	if _, err := testApp.InitChain(&abci.RequestInitChain{
		ChainId:         blockSTMCosmosFeeBenchmarkChainID,
		InitialHeight:   1,
		Time:            baseFixture.startTime,
		AppStateBytes:   appState,
		ConsensusParams: &consensusParams,
	}); err != nil {
		b.Fatalf("InitChain standard MsgSend gas benchmark app: %v", err)
	}
	initialBlock, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   baseFixture.startTime.Add(time.Second),
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

func (fixture *standardMsgSendGasBenchmarkFixture) signedBlock(
	b *testing.B,
	topology standardMsgSendGasBenchmarkTopology,
	txCount int,
	blockIndex int,
) [][]byte {
	b.Helper()

	txs := make([][]byte, txCount)
	sequence := fixture.nextSequence + uint64(blockIndex)
	for txIndex := 0; txIndex < txCount; txIndex++ {
		recipient := fixture.sharedRecipient
		if topology == standardMsgSendGasBenchmarkIndependent {
			recipient = fixture.independentRecipients[txIndex]
		}
		txs[txIndex] = fixture.signTx(b, fixture.signers[txIndex], recipient, sequence)
	}
	return txs
}

func (fixture *standardMsgSendGasBenchmarkFixture) signTx(
	b *testing.B,
	signer blockSTMCosmosFeeBenchmarkAccount,
	recipient sdk.AccAddress,
	sequence uint64,
) []byte {
	b.Helper()

	builder := fixture.base.app.TxConfig().NewTxBuilder()
	msg := banktypes.NewMsgSend(
		signer.address,
		recipient,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
	)
	if err := builder.SetMsgs(msg); err != nil {
		b.Fatalf("set standard MsgSend: %v", err)
	}
	builder.SetGasLimit(appante.StandardMsgSendGas)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(
		appparams.BaseDenom,
		standardMsgSendGasBenchmarkFee,
	)))

	signMode, err := authsigning.APISignModeToInternal(
		fixture.base.app.TxConfig().SignModeHandler().DefaultMode(),
	)
	if err != nil {
		b.Fatalf("resolve standard MsgSend sign mode: %v", err)
	}
	signature := signing.SignatureV2{
		PubKey: signer.priv.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode: signMode,
		},
		Sequence: sequence,
	}
	if err := builder.SetSignatures(signature); err != nil {
		b.Fatalf("set standard MsgSend placeholder signature: %v", err)
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
		fixture.base.app.TxConfig().SignModeHandler(),
		signMode,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		b.Fatalf("build standard MsgSend sign bytes: %v", err)
	}
	rawSignature, err := signer.priv.Sign(signBytes)
	if err != nil {
		b.Fatalf("sign standard MsgSend transaction: %v", err)
	}
	signature.Data.(*signing.SingleSignatureData).Signature = rawSignature
	if err := builder.SetSignatures(signature); err != nil {
		b.Fatalf("set standard MsgSend final signature: %v", err)
	}

	txBytes, err := fixture.base.app.TxConfig().TxEncoder()(builder.GetTx())
	if err != nil {
		b.Fatalf("encode standard MsgSend transaction: %v", err)
	}
	return txBytes
}
