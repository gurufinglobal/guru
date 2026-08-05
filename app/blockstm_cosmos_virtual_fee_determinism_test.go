package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	server "github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

const (
	blockSTMCosmosVirtualFeeWorkerEnv = "GURU_BLOCKSTM_COSMOS_VIRTUAL_FEE_WORKER"
	blockSTMCosmosVirtualFeeOutputEnv = "GURU_BLOCKSTM_COSMOS_VIRTUAL_FEE_OUTPUT"
)

type blockSTMCosmosVirtualFeeExecutor struct {
	name        string
	executor    string
	workers     int
	preEstimate bool
}

type blockSTMCosmosVirtualFeeOutcome struct {
	finalize          *abci.ResponseFinalizeBlock
	commitHash        []byte
	balances          map[string]string
	sequences         map[string]uint64
	allowances        map[string]string
	emptyFinalize     *abci.ResponseFinalizeBlock
	emptyCommitHash   []byte
	emptyBalances     map[string]string
	emptySequences    map[string]uint64
	emptyAllowances   map[string]string
	collectorAfterTxs string
	collectorAfterGap string
}

type blockSTMCosmosVirtualFeeProcessResult struct {
	GenesisHash []byte                      `json:"genesis_hash"`
	Txs         [][]byte                    `json:"txs"`
	Finalize    *abci.ResponseFinalizeBlock `json:"finalize"`
	CommitHash  []byte                      `json:"commit_hash"`
	Balances    map[string]string           `json:"balances"`
	Sequences   map[string]uint64           `json:"sequences"`
	Allowances  map[string]string           `json:"allowances"`
	Empty       *abci.ResponseFinalizeBlock `json:"empty_finalize"`
	EmptyHash   []byte                      `json:"empty_commit_hash"`
	EmptyFunds  map[string]string           `json:"empty_balances"`
	EmptySeqs   map[string]uint64           `json:"empty_sequences"`
	EmptyGrants map[string]string           `json:"empty_allowances"`
}

// TestBlockSTMCosmosVirtualFeeDeterminism is a differential consensus test. It
// initializes independent applications from identical genesis state and feeds
// each one the exact same ordered signed transaction bytes. Sequential execution
// is the oracle; every supported BlockSTM worker/pre-estimation combination must
// produce byte-for-byte equivalent transaction results and identical state roots.
func TestBlockSTMCosmosVirtualFeeDeterminism(t *testing.T) {
	executors := blockSTMCosmosVirtualFeeExecutors()
	if rawIndex := os.Getenv(blockSTMCosmosVirtualFeeWorkerEnv); rawIndex != "" {
		index, err := strconv.Atoi(rawIndex)
		require.NoError(t, err)
		require.GreaterOrEqual(t, index, 0)
		require.Less(t, index, len(executors))
		runBlockSTMCosmosVirtualFeeWorker(t, executors[index])
		return
	}

	results := make([]blockSTMCosmosVirtualFeeProcessResult, len(executors))
	outputDir := t.TempDir()
	for index, executor := range executors {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("executor-%d.json", index))
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
		cmd.Env = append(
			os.Environ(),
			blockSTMCosmosVirtualFeeWorkerEnv+"="+strconv.Itoa(index),
			blockSTMCosmosVirtualFeeOutputEnv+"="+outputPath,
		)
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "%s subprocess failed:\n%s", executor.name, output)
		encoded, err := os.ReadFile(outputPath)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encoded, &results[index]))
	}

	reference := results[0]
	require.NotEmpty(t, reference.GenesisHash)
	for index := 1; index < len(results); index++ {
		executor := executors[index]
		candidate := results[index]
		require.Equalf(t, reference.GenesisHash, candidate.GenesisHash, "%s genesis AppHash", executor.name)
		require.Equalf(t, reference.Txs, candidate.Txs, "%s exact ordered signed transaction bytes", executor.name)
		require.Equalf(t, reference.Finalize.TxResults, candidate.Finalize.TxResults, "%s transaction results (including gas and events)", executor.name)
		require.Equalf(t, reference.Finalize.Events, candidate.Finalize.Events, "%s block events", executor.name)
		require.Equalf(t, reference.Finalize.AppHash, candidate.Finalize.AppHash, "%s FinalizeBlock AppHash", executor.name)
		require.Equalf(t, reference.CommitHash, candidate.CommitHash, "%s committed AppHash", executor.name)
		require.Equalf(t, reference.Balances, candidate.Balances, "%s post-block balances", executor.name)
		require.Equalf(t, reference.Allowances, candidate.Allowances, "%s post-block feegrant allowances", executor.name)
		require.Equalf(t, reference.Sequences, candidate.Sequences, "%s post-block account sequences", executor.name)

		require.Equalf(t, reference.Empty.TxResults, candidate.Empty.TxResults, "%s empty-block transaction results", executor.name)
		require.Equalf(t, reference.Empty.Events, candidate.Empty.Events, "%s empty-block events", executor.name)
		require.Equalf(t, reference.Empty.AppHash, candidate.Empty.AppHash, "%s empty-block FinalizeBlock AppHash", executor.name)
		require.Equalf(t, reference.EmptyHash, candidate.EmptyHash, "%s empty-block committed AppHash", executor.name)
		require.Equalf(t, reference.EmptyFunds, candidate.EmptyFunds, "%s post-empty-block balances", executor.name)
		require.Equalf(t, reference.EmptyGrants, candidate.EmptyGrants, "%s post-empty-block feegrant allowances", executor.name)
		require.Equalf(t, reference.EmptySeqs, candidate.EmptySeqs, "%s post-empty-block account sequences", executor.name)
	}
}

func blockSTMCosmosVirtualFeeExecutors() []blockSTMCosmosVirtualFeeExecutor {
	return []blockSTMCosmosVirtualFeeExecutor{
		{name: "sequential", executor: serverconfig.BlockExecutorSequential},
		{name: "block-stm/workers=1/pre-estimate=false", executor: serverconfig.BlockExecutorBlockSTM, workers: 1, preEstimate: false},
		{name: "block-stm/workers=1/pre-estimate=true", executor: serverconfig.BlockExecutorBlockSTM, workers: 1, preEstimate: true},
		{name: "block-stm/workers=4/pre-estimate=false", executor: serverconfig.BlockExecutorBlockSTM, workers: 4, preEstimate: false},
		{name: "block-stm/workers=4/pre-estimate=true", executor: serverconfig.BlockExecutorBlockSTM, workers: 4, preEstimate: true},
	}
}

func runBlockSTMCosmosVirtualFeeWorker(t *testing.T, executor blockSTMCosmosVirtualFeeExecutor) {
	t.Helper()
	genesisOptions := feePolicyRuntimeGenesisOptions{
		noBaseFee:   true,
		baseFee:     sdkmath.LegacyZeroDec(),
		minGasPrice: sdkmath.LegacyOneDec(),
	}
	fixture := newBlockSTMCosmosVirtualFeeFixture(t, genesisOptions, executor)
	genesisHash := append([]byte(nil), fixture.app.LastCommitID().Hash...)
	require.NotEmpty(t, genesisHash)
	txs := blockSTMCosmosVirtualFeeTransactions(t, fixture)
	outcome := executeBlockSTMCosmosVirtualFeeScenario(t, fixture, txs)
	requireBlockSTMCosmosVirtualFeeSemantics(t, fixture, outcome)

	result := blockSTMCosmosVirtualFeeProcessResult{
		GenesisHash: genesisHash,
		Txs:         txs,
		Finalize:    outcome.finalize,
		CommitHash:  outcome.commitHash,
		Balances:    outcome.balances,
		Sequences:   outcome.sequences,
		Allowances:  outcome.allowances,
		Empty:       outcome.emptyFinalize,
		EmptyHash:   outcome.emptyCommitHash,
		EmptyFunds:  outcome.emptyBalances,
		EmptySeqs:   outcome.emptySequences,
		EmptyGrants: outcome.emptyAllowances,
	}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	outputPath := os.Getenv(blockSTMCosmosVirtualFeeOutputEnv)
	require.NotEmpty(t, outputPath)
	require.NoError(t, os.WriteFile(outputPath, encoded, 0o600))
}

func newBlockSTMCosmosVirtualFeeFixture(
	t *testing.T,
	genesisOptions feePolicyRuntimeGenesisOptions,
	executor blockSTMCosmosVirtualFeeExecutor,
) *feePolicyRuntimeFixture {
	t.Helper()
	configureFeePolicyTestBech32Prefixes(t, false)

	appOptions := feePolicyTestAppOptions()
	appOptions[server.FlagBlockExecutor] = executor.executor
	appOptions[server.FlagBlockSTMWorkers] = executor.workers
	appOptions[server.FlagBlockSTMPreEstimate] = executor.preEstimate
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		appOptions,
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
		panic("blockstm virtual-fee determinism fixture only supports canonical fee-market genesis")
	}

	return fixture
}

func blockSTMCosmosVirtualFeeTransactions(t *testing.T, fixture *feePolicyRuntimeFixture) [][]byte {
	t.Helper()
	recipient := fixture.actor(feePolicyActorRecipient)
	normal := fixture.actor(feePolicyActorNormal)
	grantPayer := fixture.actor(feePolicyActorGrantPayer)
	grantGranter := fixture.actor(feePolicyActorGrantGranter)
	messageFailurePayer := fixture.actor(feePolicyActorGrantMsgPayer)
	messageFailureGranter := fixture.actor(feePolicyActorGrantMsgGranter)
	signatureFailurePayer := fixture.actor(feePolicyActorGrantSigPayer)
	signatureFailureGranter := fixture.actor(feePolicyActorGrantSigGranter)

	return [][]byte{
		fixture.signTx(t, normal, []sdk.Msg{
			banktypes.NewMsgSend(normal.address, recipient.address, feePolicyRuntimeCoins(7)),
		}, feePolicyRuntimeTxOptions{}),
		fixture.signTx(t, grantPayer, []sdk.Msg{
			banktypes.NewMsgSend(grantPayer.address, recipient.address, feePolicyRuntimeCoins(1)),
		}, feePolicyRuntimeTxOptions{feeGranter: grantGranter.address}),
		fixture.signTx(t, messageFailurePayer, []sdk.Msg{
			banktypes.NewMsgSend(
				messageFailurePayer.address,
				recipient.address,
				feePolicyRuntimeCoins(feePolicyRuntimeInitialFunds+1),
			),
		}, feePolicyRuntimeTxOptions{feeGranter: messageFailureGranter.address}),
		fixture.signTx(t, signatureFailurePayer, []sdk.Msg{
			banktypes.NewMsgSend(signatureFailurePayer.address, recipient.address, feePolicyRuntimeCoins(1)),
		}, feePolicyRuntimeTxOptions{feeGranter: signatureFailureGranter.address, corruptSig: true}),
	}
}

func executeBlockSTMCosmosVirtualFeeScenario(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	txs [][]byte,
) blockSTMCosmosVirtualFeeOutcome {
	t.Helper()

	response, err := fixture.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: fixture.nextBlock,
		Time:   fixture.startTime.Add(time.Duration(fixture.nextBlock) * time.Second),
		Txs:    txs,
	})
	require.NoError(t, err)
	require.Len(t, response.TxResults, len(txs))
	_, err = fixture.app.Commit()
	require.NoError(t, err)
	fixture.nextBlock++

	commitHash := append([]byte(nil), fixture.app.LastCommitID().Hash...)
	require.Equal(t, response.AppHash, commitHash, "FinalizeBlock and Commit must expose the same state root")
	balances, sequences, allowances := blockSTMCosmosVirtualFeeState(t, fixture)
	collectorAfterTxs := fixture.collectorBalance().String()

	emptyResponse, err := fixture.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: fixture.nextBlock,
		Time:   fixture.startTime.Add(time.Duration(fixture.nextBlock) * time.Second),
	})
	require.NoError(t, err)
	require.Empty(t, emptyResponse.TxResults)
	_, err = fixture.app.Commit()
	require.NoError(t, err)
	fixture.nextBlock++

	emptyCommitHash := append([]byte(nil), fixture.app.LastCommitID().Hash...)
	require.Equal(t, emptyResponse.AppHash, emptyCommitHash, "empty FinalizeBlock and Commit must expose the same state root")
	emptyBalances, emptySequences, emptyAllowances := blockSTMCosmosVirtualFeeState(t, fixture)
	collectorAfterGap := fixture.collectorBalance().String()

	return blockSTMCosmosVirtualFeeOutcome{
		finalize:          response,
		commitHash:        commitHash,
		balances:          balances,
		sequences:         sequences,
		allowances:        allowances,
		emptyFinalize:     emptyResponse,
		emptyCommitHash:   emptyCommitHash,
		emptyBalances:     emptyBalances,
		emptySequences:    emptySequences,
		emptyAllowances:   emptyAllowances,
		collectorAfterTxs: collectorAfterTxs,
		collectorAfterGap: collectorAfterGap,
	}
}

func blockSTMCosmosVirtualFeeState(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
) (map[string]string, map[string]uint64, map[string]string) {
	t.Helper()

	balances := make(map[string]string, len(feePolicyRuntimeActorNames)+1)
	sequences := make(map[string]uint64, len(feePolicyRuntimeActorNames))
	for _, name := range feePolicyRuntimeActorNames {
		actor := fixture.actor(name)
		balances[name] = fixture.balance(actor.address).String()
		sequences[name] = fixture.sequence(actor.address)
	}
	balances[authtypes.FeeCollectorName] = fixture.collectorBalance().String()

	allowances := map[string]string{
		"successful": fixture.allowance(
			t,
			fixture.actor(feePolicyActorGrantGranter),
			fixture.actor(feePolicyActorGrantPayer),
		).String(),
		"message-failure": fixture.allowance(
			t,
			fixture.actor(feePolicyActorGrantMsgGranter),
			fixture.actor(feePolicyActorGrantMsgPayer),
		).String(),
		"signature-failure": fixture.allowance(
			t,
			fixture.actor(feePolicyActorGrantSigGranter),
			fixture.actor(feePolicyActorGrantSigPayer),
		).String(),
	}
	return balances, sequences, allowances
}

func requireBlockSTMCosmosVirtualFeeSemantics(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	outcome blockSTMCosmosVirtualFeeOutcome,
) {
	t.Helper()
	require.Len(t, outcome.finalize.TxResults, 4)
	require.Equal(t, abci.CodeTypeOK, outcome.finalize.TxResults[0].Code, outcome.finalize.TxResults[0].Log)
	require.Equal(t, abci.CodeTypeOK, outcome.finalize.TxResults[1].Code, outcome.finalize.TxResults[1].Log)
	require.NotEqual(t, abci.CodeTypeOK, outcome.finalize.TxResults[2].Code)
	require.Contains(t, outcome.finalize.TxResults[2].Log, "insufficient funds")
	require.NotEqual(t, abci.CodeTypeOK, outcome.finalize.TxResults[3].Code)
	require.Contains(t, outcome.finalize.TxResults[3].Log, "signature verification failed")

	normal := fixture.actor(feePolicyActorNormal)
	grantGranter := fixture.actor(feePolicyActorGrantGranter)
	messageFailureGranter := fixture.actor(feePolicyActorGrantMsgGranter)
	signatureFailureGranter := fixture.actor(feePolicyActorGrantSigGranter)

	fixture.requireFeeEvent(t, outcome.finalize.TxResults[0], 150_000, normal.addressString)
	fixture.requireFeeEvent(t, outcome.finalize.TxResults[1], 150_000, grantGranter.addressString)
	fixture.requireFeeEvent(t, outcome.finalize.TxResults[2], 150_000, messageFailureGranter.addressString)
	fixture.requireNoFeeEvent(t, outcome.finalize.TxResults[3])

	collectorAddress := fixture.app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	collectorAddressString, err := fixture.app.AccountKeeper.AddressCodec().BytesToString(collectorAddress)
	require.NoError(t, err)
	fee := feePolicyRuntimeCoins(150_000).String()
	for index, payer := range []string{
		normal.addressString,
		grantGranter.addressString,
		messageFailureGranter.addressString,
	} {
		result := outcome.finalize.TxResults[index]
		requireABCIEvent(t, result.Events, banktypes.EventTypeCoinSpent, map[string]string{
			banktypes.AttributeKeySpender: payer,
			sdk.AttributeKeyAmount:        fee,
		})
		requireABCIEvent(t, result.Events, banktypes.EventTypeTransfer, map[string]string{
			banktypes.AttributeKeyRecipient: collectorAddressString,
			banktypes.AttributeKeySender:    payer,
			sdk.AttributeKeyAmount:          fee,
		})
		requireABCIEvent(t, result.Events, sdk.EventTypeMessage, map[string]string{
			sdk.AttributeKeySender: payer,
		})
		requireNoABCIEvent(t, result.Events, banktypes.EventTypeCoinReceived, map[string]string{
			banktypes.AttributeKeyReceiver: collectorAddressString,
		})
	}
	requireNoABCIEvent(t, outcome.finalize.TxResults[3].Events, banktypes.EventTypeCoinSpent, map[string]string{
		banktypes.AttributeKeySpender: signatureFailureGranter.addressString,
	})

	totalFee := feePolicyRuntimeCoins(450_000).String()
	requireABCIEvent(t, outcome.finalize.Events, banktypes.EventTypeCoinReceived, map[string]string{
		banktypes.AttributeKeyReceiver: collectorAddressString,
		sdk.AttributeKeyAmount:         totalFee,
	})
	requireNoABCIEvent(t, outcome.emptyFinalize.Events, banktypes.EventTypeCoinReceived, map[string]string{
		banktypes.AttributeKeyReceiver: collectorAddressString,
		sdk.AttributeKeyAmount:         totalFee,
	})

	require.Equal(t, sdkmath.NewInt(1_849_993).String(), outcome.balances[feePolicyActorNormal])
	require.Equal(t, sdkmath.NewInt(1_999_999).String(), outcome.balances[feePolicyActorGrantPayer])
	require.Equal(t, sdkmath.NewInt(1_850_000).String(), outcome.balances[feePolicyActorGrantGranter])
	require.Equal(t, sdkmath.NewInt(feePolicyRuntimeInitialFunds).String(), outcome.balances[feePolicyActorGrantMsgPayer])
	require.Equal(t, sdkmath.NewInt(1_850_000).String(), outcome.balances[feePolicyActorGrantMsgGranter])
	require.Equal(t, sdkmath.NewInt(feePolicyRuntimeInitialFunds).String(), outcome.balances[feePolicyActorGrantSigPayer])
	require.Equal(t, sdkmath.NewInt(feePolicyRuntimeInitialFunds).String(), outcome.balances[feePolicyActorGrantSigGranter])
	require.Equal(t, sdkmath.NewInt(8).String(), outcome.balances[feePolicyActorRecipient])
	require.Equal(t, sdkmath.NewInt(450_000).String(), outcome.balances[authtypes.FeeCollectorName])

	require.Equal(t, uint64(1), outcome.sequences[feePolicyActorNormal])
	require.Equal(t, uint64(1), outcome.sequences[feePolicyActorGrantPayer])
	require.Equal(t, uint64(1), outcome.sequences[feePolicyActorGrantMsgPayer])
	require.Equal(t, uint64(0), outcome.sequences[feePolicyActorGrantSigPayer])
	require.Equal(t, "250000", outcome.allowances["successful"])
	require.Equal(t, "250000", outcome.allowances["message-failure"])
	require.Equal(t, "400000", outcome.allowances["signature-failure"])

	for _, name := range feePolicyRuntimeActorNames {
		require.Equalf(t, outcome.balances[name], outcome.emptyBalances[name], "empty block actor balance %s", name)
	}
	require.Equal(t, outcome.sequences, outcome.emptySequences, "the empty block must not change actor sequences")
	require.Equal(t, outcome.allowances, outcome.emptyAllowances, "the empty block must not consume feegrant twice")
	require.Equal(t, sdkmath.NewInt(450_000).String(), outcome.collectorAfterTxs)
	require.Equal(t, "0", outcome.collectorAfterGap, "distribution must drain the prior block's settled fees before bank EndBlock; stale virtual entries would re-credit them")
	require.Equal(t, sdkmath.NewInt(8).String(), outcome.emptyBalances[feePolicyActorRecipient])
}

func requireABCIEvent(t *testing.T, events []abci.Event, eventType string, attributes map[string]string) {
	t.Helper()
	require.Truef(t, hasABCIEvent(events, eventType, attributes), "missing %s event with attributes %v in %v", eventType, attributes, events)
}

func requireNoABCIEvent(t *testing.T, events []abci.Event, eventType string, attributes map[string]string) {
	t.Helper()
	require.Falsef(t, hasABCIEvent(events, eventType, attributes), "unexpected %s event with attributes %v in %v", eventType, attributes, events)
}

func hasABCIEvent(events []abci.Event, eventType string, attributes map[string]string) bool {
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		matched := true
		for key, expected := range attributes {
			found := false
			for _, attribute := range event.Attributes {
				if attribute.Key == key && attribute.Value == expected {
					found = true
					break
				}
			}
			if !found {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
