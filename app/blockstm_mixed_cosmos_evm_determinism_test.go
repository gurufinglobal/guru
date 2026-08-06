package app

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
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
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	srvflags "github.com/cosmos/evm/server/flags"
	evmtesttx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
)

const (
	blockSTMMixedCosmosEVMWorkerEnv = "GURU_BLOCKSTM_MIXED_COSMOS_EVM_WORKER"
	blockSTMMixedCosmosEVMOutputEnv = "GURU_BLOCKSTM_MIXED_COSMOS_EVM_OUTPUT"

	blockSTMMixedEVMInitialFunds = int64(2_000_000)
	blockSTMMixedEVMTransfer     = int64(17)
	blockSTMMixedEVMRevertValue  = int64(11)

	blockSTMMixedEVMDeployGas   = uint64(150_000)
	blockSTMMixedEVMLogGas      = uint64(100_000)
	blockSTMMixedEVMRevertGas   = uint64(100_000)
	blockSTMMixedEVMTransferGas = uint64(21_000)
)

var (
	// Empty calldata emits two LOG0 records and succeeds; non-empty calldata
	// reverts. This keeps both log-index patching and refund/rollback behavior in
	// one same-block contract dependency chain.
	blockSTMMixedReverterInitCode = mustBlockSTMMixedHex("6016600c60003960166000f33615600a5760006000fd5b60006000a060006000a000")
	blockSTMMixedReverterRuntime  = mustBlockSTMMixedHex("3615600a5760006000fd5b60006000a060006000a000")
)

type blockSTMMixedCosmosEVMFixture struct {
	*feePolicyRuntimeFixture
	evmPrivateKey *ethsecp256k1.PrivKey
	evmAddress    sdk.AccAddress
	ethAddress    common.Address
	contract      common.Address
	evmTxHashes   []string
}

type blockSTMMixedCosmosEVMState struct {
	EVMSenderBalance      string `json:"evm_sender_balance"`
	RecipientBalance      string `json:"recipient_balance"`
	CollectorBalance      string `json:"collector_balance"`
	EVMSenderNonce        uint64 `json:"evm_sender_nonce"`
	ContractBalance       string `json:"contract_balance"`
	ContractCode          string `json:"contract_code"`
	CosmosNormalBalance   string `json:"cosmos_normal_balance"`
	CosmosFailedBalance   string `json:"cosmos_failed_balance"`
	CosmosGranterBalance  string `json:"cosmos_granter_balance"`
	CosmosFailedAllowance string `json:"cosmos_failed_allowance"`
}

type blockSTMMixedCosmosEVMProcessResult struct {
	GenesisHash []byte                      `json:"genesis_hash"`
	Txs         [][]byte                    `json:"txs"`
	Finalize    *abci.ResponseFinalizeBlock `json:"finalize"`
	CommitHash  []byte                      `json:"commit_hash"`
	State       blockSTMMixedCosmosEVMState `json:"state"`
	Empty       *abci.ResponseFinalizeBlock `json:"empty_finalize"`
	EmptyHash   []byte                      `json:"empty_commit_hash"`
	EmptyState  blockSTMMixedCosmosEVMState `json:"empty_state"`
}

// TestBlockSTMMixedCosmosEVMTxDeterminism feeds the same ordered, signed Cosmos
// and Ethereum transaction bytes to independent sequential and BlockSTM apps.
// It covers both virtual-fee paths sharing one fee collector, EVM log-index
// patching, unused-gas refunds, an EVM revert, and same-sender/same-block EVM
// nonce dependencies.
func TestBlockSTMMixedCosmosEVMTxDeterminism(t *testing.T) {
	executors := blockSTMCosmosVirtualFeeExecutors()
	if rawIndex := os.Getenv(blockSTMMixedCosmosEVMWorkerEnv); rawIndex != "" {
		index, err := strconv.Atoi(rawIndex)
		require.NoError(t, err)
		require.GreaterOrEqual(t, index, 0)
		require.Less(t, index, len(executors))
		runBlockSTMMixedCosmosEVMWorker(t, executors[index])
		return
	}

	results := make([]blockSTMMixedCosmosEVMProcessResult, len(executors))
	outputDir := t.TempDir()
	for index, executor := range executors {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("executor-%d.json", index))
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
		cmd.Env = append(
			os.Environ(),
			blockSTMMixedCosmosEVMWorkerEnv+"="+strconv.Itoa(index),
			blockSTMMixedCosmosEVMOutputEnv+"="+outputPath,
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
		require.Equalf(t, reference.Finalize, candidate.Finalize, "%s complete FinalizeBlock response (including tx results, EVM response patching, events, validator updates, consensus updates, and AppHash)", executor.name)
		require.Equalf(t, reference.CommitHash, candidate.CommitHash, "%s committed AppHash", executor.name)
		require.Equalf(t, reference.State, candidate.State, "%s post-block mixed Cosmos/EVM state", executor.name)

		require.Equalf(t, reference.Empty, candidate.Empty, "%s complete empty FinalizeBlock response", executor.name)
		require.Equalf(t, reference.EmptyHash, candidate.EmptyHash, "%s empty-block committed AppHash", executor.name)
		require.Equalf(t, reference.EmptyState, candidate.EmptyState, "%s post-empty-block mixed Cosmos/EVM state", executor.name)
	}
}

func runBlockSTMMixedCosmosEVMWorker(t *testing.T, executor blockSTMCosmosVirtualFeeExecutor) {
	t.Helper()
	fixture := newBlockSTMMixedCosmosEVMFixture(t, executor)
	genesisHash := append([]byte(nil), fixture.app.LastCommitID().Hash...)
	require.NotEmpty(t, genesisHash)

	txs := blockSTMMixedCosmosEVMTransactions(t, fixture)
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
	require.Equal(t, response.AppHash, commitHash)
	state := blockSTMMixedCosmosEVMCommittedState(t, fixture)
	requireBlockSTMMixedCosmosEVMSemantics(t, fixture, response, state)

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
	require.Equal(t, emptyResponse.AppHash, emptyCommitHash)
	emptyState := blockSTMMixedCosmosEVMCommittedState(t, fixture)
	requireBlockSTMMixedCosmosEVMEmptyBlockSemantics(t, state, emptyState)

	result := blockSTMMixedCosmosEVMProcessResult{
		GenesisHash: genesisHash,
		Txs:         txs,
		Finalize:    response,
		CommitHash:  commitHash,
		State:       state,
		Empty:       emptyResponse,
		EmptyHash:   emptyCommitHash,
		EmptyState:  emptyState,
	}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	outputPath := os.Getenv(blockSTMMixedCosmosEVMOutputEnv)
	require.NotEmpty(t, outputPath)
	require.NoError(t, os.WriteFile(outputPath, encoded, 0o600))
}

func newBlockSTMMixedCosmosEVMFixture(
	t *testing.T,
	executor blockSTMCosmosVirtualFeeExecutor,
) *blockSTMMixedCosmosEVMFixture {
	t.Helper()
	configureFeePolicyTestBech32Prefixes(t, false)

	appOptions := feePolicyTestAppOptions()
	appOptions[srvflags.EVMChainID] = appparams.EVMChainID
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

	baseFixture := &feePolicyRuntimeFixture{
		app:       testApp,
		actors:    make(map[string]feePolicyRuntimeSigner, len(feePolicyRuntimeActorNames)),
		nextBlock: 2,
		startTime: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
	}
	for accountNumber, name := range feePolicyRuntimeActorNames {
		baseFixture.actors[name] = newFeePolicyRuntimeSigner(t, testApp, name, uint64(accountNumber))
	}

	privateKeyBytes := make([]byte, ethsecp256k1.PrivKeySize)
	privateKeyBytes[len(privateKeyBytes)-1] = 1
	evmPrivateKey := &ethsecp256k1.PrivKey{Key: privateKeyBytes}
	ethAddress := common.BytesToAddress(evmPrivateKey.PubKey().Address().Bytes())
	evmAddress := sdk.AccAddress(ethAddress.Bytes())
	fixture := &blockSTMMixedCosmosEVMFixture{
		feePolicyRuntimeFixture: baseFixture,
		evmPrivateKey:           evmPrivateKey,
		evmAddress:              evmAddress,
		ethAddress:              ethAddress,
		contract:                crypto.CreateAddress(ethAddress, 0),
		evmTxHashes:             make([]string, 0, 5),
	}

	genesis := testApp.BuildChainDefaultGenesis()
	setFeePolicyRuntimeConstitutionGenesis(t, baseFixture, genesis)
	setFeePolicyRuntimeAuthGenesis(t, baseFixture, genesis)
	setFeePolicyRuntimeStakingGenesis(t, baseFixture, genesis)
	setFeePolicyRuntimeBankGenesis(t, baseFixture, genesis)
	setFeePolicyRuntimeFeeMarketGenesis(t, baseFixture, genesis)
	setFeePolicyRuntimeMintGenesis(t, baseFixture, genesis)
	setFeePolicyRuntimePolicyGenesis(t, baseFixture, genesis)
	setFeePolicyRuntimeFeeGrantGenesis(t, baseFixture, genesis)
	addBlockSTMMixedEVMGenesisAccount(t, fixture, genesis)
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
	require.Equal(t, sdkmath.NewInt(blockSTMMixedEVMInitialFunds), fixture.balance(evmAddress))
	return fixture
}

func addBlockSTMMixedEVMGenesisAccount(
	t *testing.T,
	fixture *blockSTMMixedCosmosEVMFixture,
	genesis map[string]json.RawMessage,
) {
	t.Helper()

	authGenesis := authtypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[authtypes.ModuleName], authGenesis)
	accounts, err := authtypes.UnpackAccounts(authGenesis.Accounts)
	require.NoError(t, err)
	accounts = append(accounts, authtypes.NewBaseAccount(
		fixture.evmAddress,
		fixture.evmPrivateKey.PubKey(),
		uint64(len(feePolicyRuntimeActorNames)),
		0,
	))
	authGenesis = authtypes.NewGenesisState(authGenesis.Params, accounts)
	genesis[authtypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(authGenesis)

	bankGenesis := banktypes.DefaultGenesisState()
	fixture.app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], bankGenesis)
	evmAddressString, err := fixture.app.AccountKeeper.AddressCodec().BytesToString(fixture.evmAddress)
	require.NoError(t, err)
	bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
		Address: evmAddressString,
		Coins:   feePolicyRuntimeCoins(blockSTMMixedEVMInitialFunds),
	})
	bankGenesis.Supply = nil
	genesis[banktypes.ModuleName] = fixture.app.AppCodec().MustMarshalJSON(bankGenesis)
}

func blockSTMMixedCosmosEVMTransactions(
	t *testing.T,
	fixture *blockSTMMixedCosmosEVMFixture,
) [][]byte {
	t.Helper()
	normal := fixture.actor(feePolicyActorNormal)
	recipient := fixture.actor(feePolicyActorRecipient)
	messageFailurePayer := fixture.actor(feePolicyActorGrantMsgPayer)
	messageFailureGranter := fixture.actor(feePolicyActorGrantMsgGranter)

	return [][]byte{
		fixture.signTx(t, normal, []sdk.Msg{
			banktypes.NewMsgSend(normal.address, recipient.address, feePolicyRuntimeCoins(7)),
		}, feePolicyRuntimeTxOptions{}),
		fixture.signEVMTransaction(t, 0, nil, 0, blockSTMMixedEVMDeployGas, blockSTMMixedReverterInitCode),
		fixture.signEVMTransaction(t, 1, &fixture.contract, 0, blockSTMMixedEVMLogGas, nil),
		fixture.signTx(t, messageFailurePayer, []sdk.Msg{
			banktypes.NewMsgSend(
				messageFailurePayer.address,
				recipient.address,
				feePolicyRuntimeCoins(feePolicyRuntimeInitialFunds+1),
			),
		}, feePolicyRuntimeTxOptions{feeGranter: messageFailureGranter.address}),
		fixture.signEVMTransaction(t, 2, &fixture.contract, 0, blockSTMMixedEVMLogGas, nil),
		fixture.signEVMTransaction(t, 3, &fixture.contract, blockSTMMixedEVMRevertValue, blockSTMMixedEVMRevertGas, []byte{0x01}),
		fixture.signEVMTransaction(
			t,
			4,
			blockSTMMixedCommonAddress(recipient.address),
			blockSTMMixedEVMTransfer,
			blockSTMMixedEVMTransferGas,
			nil,
		),
	}
}

func (fixture *blockSTMMixedCosmosEVMFixture) signEVMTransaction(
	t *testing.T,
	nonce uint64,
	to *common.Address,
	amount int64,
	gasLimit uint64,
	input []byte,
) []byte {
	t.Helper()

	msg := evmtypes.NewTx(&evmtypes.EvmTxArgs{
		ChainID:  new(big.Int).SetUint64(appparams.EVMChainID),
		Nonce:    nonce,
		To:       to,
		Amount:   big.NewInt(amount),
		GasLimit: gasLimit,
		GasPrice: big.NewInt(1),
		Input:    append([]byte(nil), input...),
	})
	msg.From = fixture.ethAddress.Bytes()
	signer := gethtypes.LatestSignerForChainID(new(big.Int).SetUint64(appparams.EVMChainID))
	require.NoError(t, msg.Sign(signer, evmtesttx.NewSigner(fixture.evmPrivateKey)))
	require.NoError(t, msg.ValidateBasic())

	tx, err := msg.BuildTxWithEvmParams(fixture.app.TxConfig().NewTxBuilder(), evmtypes.Params{
		EvmDenom: appparams.BaseDenom,
		ExtendedDenomOptions: &evmtypes.ExtendedDenomOptions{
			ExtendedDenom: appparams.BaseDenom,
		},
	})
	require.NoError(t, err)
	txBytes, err := fixture.app.TxConfig().TxEncoder()(tx)
	require.NoError(t, err)
	fixture.evmTxHashes = append(fixture.evmTxHashes, msg.Hash().Hex())
	return txBytes
}

func blockSTMMixedCosmosEVMCommittedState(
	t *testing.T,
	fixture *blockSTMMixedCosmosEVMFixture,
) blockSTMMixedCosmosEVMState {
	t.Helper()
	ctx := fixture.committedContext()
	codeHash := fixture.app.EVMKeeper.GetCodeHash(ctx, fixture.contract)
	normal := fixture.actor(feePolicyActorNormal)
	messageFailurePayer := fixture.actor(feePolicyActorGrantMsgPayer)
	messageFailureGranter := fixture.actor(feePolicyActorGrantMsgGranter)
	return blockSTMMixedCosmosEVMState{
		EVMSenderBalance:      fixture.balance(fixture.evmAddress).String(),
		RecipientBalance:      fixture.balance(fixture.actor(feePolicyActorRecipient).address).String(),
		CollectorBalance:      fixture.collectorBalance().String(),
		EVMSenderNonce:        fixture.app.EVMKeeper.GetNonce(ctx, fixture.ethAddress),
		ContractBalance:       fixture.app.EVMKeeper.GetBalance(ctx, fixture.contract).String(),
		ContractCode:          hex.EncodeToString(fixture.app.EVMKeeper.GetCode(ctx, codeHash)),
		CosmosNormalBalance:   fixture.balance(normal.address).String(),
		CosmosFailedBalance:   fixture.balance(messageFailurePayer.address).String(),
		CosmosGranterBalance:  fixture.balance(messageFailureGranter.address).String(),
		CosmosFailedAllowance: fixture.allowance(t, messageFailureGranter, messageFailurePayer).String(),
	}
}

func requireBlockSTMMixedCosmosEVMSemantics(
	t *testing.T,
	fixture *blockSTMMixedCosmosEVMFixture,
	response *abci.ResponseFinalizeBlock,
	state blockSTMMixedCosmosEVMState,
) {
	t.Helper()
	require.Len(t, response.TxResults, 7)
	require.Equal(t, abci.CodeTypeOK, response.TxResults[0].Code, response.TxResults[0].Log)
	require.Equal(t, abci.CodeTypeOK, response.TxResults[1].Code, response.TxResults[1].Log)
	require.Equal(t, abci.CodeTypeOK, response.TxResults[2].Code, response.TxResults[2].Log)
	require.NotEqual(t, abci.CodeTypeOK, response.TxResults[3].Code)
	require.Contains(t, response.TxResults[3].Log, "insufficient funds")
	require.Equal(t, abci.CodeTypeOK, response.TxResults[4].Code, response.TxResults[4].Log)
	require.Equal(t, abci.CodeTypeOK, response.TxResults[5].Code, response.TxResults[5].Log)
	require.Equal(t, abci.CodeTypeOK, response.TxResults[6].Code, response.TxResults[6].Log)

	deployResponse := blockSTMMixedEVMResponse(t, response.TxResults[1])
	firstLogResponse := blockSTMMixedEVMResponse(t, response.TxResults[2])
	secondLogResponse := blockSTMMixedEVMResponse(t, response.TxResults[4])
	revertResponse := blockSTMMixedEVMResponse(t, response.TxResults[5])
	transferResponse := blockSTMMixedEVMResponse(t, response.TxResults[6])
	require.Len(t, fixture.evmTxHashes, 5)
	require.Equal(t, fixture.evmTxHashes[0], deployResponse.Hash, "deployment response hash must identify the actual signed Ethereum transaction")
	require.Equal(t, fixture.evmTxHashes[1], firstLogResponse.Hash, "first log-call response hash must identify the actual signed Ethereum transaction")
	require.Equal(t, fixture.evmTxHashes[2], secondLogResponse.Hash, "second log-call response hash must identify the actual signed Ethereum transaction")
	require.Equal(t, fixture.evmTxHashes[3], revertResponse.Hash, "revert response hash must identify the actual signed Ethereum transaction")
	require.Equal(t, fixture.evmTxHashes[4], transferResponse.Hash, "transfer response hash must identify the actual signed Ethereum transaction")
	require.Empty(t, deployResponse.VmError)
	require.Empty(t, firstLogResponse.VmError)
	require.Empty(t, secondLogResponse.VmError)
	require.Equal(t, vm.ErrExecutionReverted.Error(), revertResponse.VmError)
	require.Empty(t, transferResponse.VmError)
	require.Positive(t, deployResponse.GasUsed)
	require.Positive(t, firstLogResponse.GasUsed)
	require.Positive(t, secondLogResponse.GasUsed)
	require.Positive(t, revertResponse.GasUsed)
	require.Positive(t, transferResponse.GasUsed)
	require.Less(t, deployResponse.GasUsed, blockSTMMixedEVMDeployGas)
	require.Less(t, firstLogResponse.GasUsed, blockSTMMixedEVMLogGas)
	require.Less(t, secondLogResponse.GasUsed, blockSTMMixedEVMLogGas)
	require.Less(t, revertResponse.GasUsed, blockSTMMixedEVMRevertGas)
	require.Equal(t, blockSTMMixedEVMTransferGas, transferResponse.GasUsed)
	require.Empty(t, deployResponse.Logs)
	require.Len(t, firstLogResponse.Logs, 2)
	require.Len(t, secondLogResponse.Logs, 2)
	require.Empty(t, revertResponse.Logs)
	require.Empty(t, transferResponse.Logs)
	for index, log := range firstLogResponse.Logs {
		require.Equal(t, fixture.contract.Hex(), log.Address)
		require.Equal(t, uint64(1), log.TxIndex, "the Cosmos transaction before this call must not consume an Ethereum transaction index")
		require.Equal(t, uint64(index), log.Index, "log indices must be cumulative and deterministic after block-level patching")
	}
	for index, log := range secondLogResponse.Logs {
		require.Equal(t, fixture.contract.Hex(), log.Address)
		require.Equal(t, uint64(2), log.TxIndex, "the intervening Cosmos transaction must not consume an Ethereum transaction index")
		require.Equal(t, uint64(index+2), log.Index, "log indices must remain cumulative across different Ethereum transactions")
	}

	totalEVMGas := deployResponse.GasUsed + firstLogResponse.GasUsed + secondLogResponse.GasUsed + revertResponse.GasUsed + transferResponse.GasUsed
	expectedEVMBalance := sdkmath.NewInt(blockSTMMixedEVMInitialFunds).
		SubRaw(int64(totalEVMGas)).
		SubRaw(blockSTMMixedEVMTransfer)
	require.Equal(t, expectedEVMBalance.String(), state.EVMSenderBalance, "unused EVM gas and reverted value must be refunded")
	require.Equal(t, sdkmath.NewInt(7+blockSTMMixedEVMTransfer).String(), state.RecipientBalance)
	require.Equal(t, uint64(5), state.EVMSenderNonce)
	require.Equal(t, "0", state.ContractBalance, "reverted EVM value transfer must roll back")
	require.Equal(t, hex.EncodeToString(blockSTMMixedReverterRuntime), state.ContractCode)
	require.Equal(t, sdkmath.NewInt(feePolicyRuntimeInitialFunds-150_000-7).String(), state.CosmosNormalBalance)
	require.Equal(t, sdkmath.NewInt(feePolicyRuntimeInitialFunds).String(), state.CosmosFailedBalance, "message failure must roll back the attempted bank transfer")
	require.Equal(t, sdkmath.NewInt(feePolicyRuntimeInitialFunds-150_000).String(), state.CosmosGranterBalance)
	require.Equal(t, sdkmath.NewInt(400_000-150_000).String(), state.CosmosFailedAllowance)
	totalFeeAmount := sdkmath.NewInt(300_000 + int64(totalEVMGas))
	totalFee := totalFeeAmount.String()
	totalFeeCoins := sdk.NewCoins(sdk.NewCoin(appparams.BaseDenom, totalFeeAmount)).String()
	require.Equal(
		t,
		totalFee,
		state.CollectorBalance,
		"Cosmos and EVM virtual fees must settle exactly once into the shared collector",
	)
	collectorAddress := fixture.app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	collectorAddressString, err := fixture.app.AccountKeeper.AddressCodec().BytesToString(collectorAddress)
	require.NoError(t, err)
	collectorAttributes := map[string]string{banktypes.AttributeKeyReceiver: collectorAddressString}
	for index, result := range response.TxResults {
		require.Zero(
			t,
			countBlockSTMMixedABCIEvents(result.Events, banktypes.EventTypeCoinReceived, collectorAttributes),
			"tx result %d must not expose an immediate fee-collector receipt while virtual collection is active",
			index,
		)
	}
	require.Equal(
		t,
		1,
		countBlockSTMMixedABCIEvents(response.Events, banktypes.EventTypeCoinReceived, map[string]string{
			banktypes.AttributeKeyReceiver: collectorAddressString,
			"mode":                         "EndBlock",
		}),
		"Cosmos and EVM virtual fees must produce one aggregate fee-collector receipt in EndBlock",
	)
	require.Equal(
		t,
		1,
		countBlockSTMMixedABCIEvents(response.Events, banktypes.EventTypeCoinReceived, map[string]string{
			banktypes.AttributeKeyReceiver: collectorAddressString,
			sdk.AttributeKeyAmount:         totalFeeCoins,
			"mode":                         "EndBlock",
		}),
		"the single EndBlock receipt must equal the combined Cosmos and EVM actual fees",
	)

	fixture.requireFeeEvent(t, response.TxResults[0], 150_000, fixture.actor(feePolicyActorNormal).addressString)
	fixture.requireFeeEvent(t, response.TxResults[3], 150_000, fixture.actor(feePolicyActorGrantMsgGranter).addressString)
}

func requireBlockSTMMixedCosmosEVMEmptyBlockSemantics(
	t *testing.T,
	state blockSTMMixedCosmosEVMState,
	emptyState blockSTMMixedCosmosEVMState,
) {
	t.Helper()
	expected := state
	expected.CollectorBalance = "0"
	require.Equal(t, expected, emptyState, "the next block may drain distribution funds but must not settle stale Cosmos or EVM virtual fees or mutate actor state")
}

func blockSTMMixedEVMResponse(t *testing.T, result *abci.ExecTxResult) *evmtypes.MsgEthereumTxResponse {
	t.Helper()
	response, err := evmtypes.DecodeTxResponse(result.Data)
	require.NoError(t, err)
	require.NotEmpty(t, response.Hash)
	return response
}

func countBlockSTMMixedABCIEvents(
	events []abci.Event,
	eventType string,
	attributes map[string]string,
) int {
	count := 0
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
			count++
		}
	}
	return count
}

func blockSTMMixedCommonAddress(address sdk.AccAddress) *common.Address {
	value := common.BytesToAddress(address)
	return &value
}

func mustBlockSTMMixedHex(value string) []byte {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return decoded
}
