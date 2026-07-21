package pulsarcompat

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gurufinglobal/guru/v3/oracle"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const envOracleConsumerCompat = "GURU_E2E_ORACLE_CONSUMER_COMPAT"

func TestE2EOracleProposalConsumerCompatibility(t *testing.T) {
	if os.Getenv(envOracleConsumerCompat) != "1" {
		t.Skipf("set %s=1 to run Oracle proposal consumer compatibility", envOracleConsumerCompat)
	}
	runE2EOracleProposalConsumerCompatibility(t, "pebbledb", false)
}

func TestE2EOracleProposalConsumerCompatibilityGoLevelDBIndexerPersistence(t *testing.T) {
	if os.Getenv(envOracleConsumerCompat) != "1" {
		t.Skipf("set %s=1 to run Oracle proposal consumer compatibility", envOracleConsumerCompat)
	}
	runE2EOracleProposalConsumerCompatibility(t, "goleveldb", true)
}

func runE2EOracleProposalConsumerCompatibility(t *testing.T, appDBBackend string, requireIndexerPersistence bool) {
	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)
	privateKey, err := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000001")
	require.NoError(t, err)
	evmAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	node, accounts := bootstrapOracleTxSmokeNetwork(t, repoRoot, bin, t.TempDir())
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", evmAddress.Hex(), "1000000000000000000000agxn", "--home", node.home)
	patchOracleConsumerGenesis(t, node.home)
	setOracleNodeAppConfig(t, node.home, node.oracleSocket, node.apiPort)
	setOracleConsumerAppDBBackend(t, node.home, appDBBackend)
	setOracleNodeCometConfig(t, node.home)
	runCmd(t, repoRoot, bin, "genesis", "validate-genesis", "--home", node.home)

	sourceServer := startOracleSoakSourceServer(t)
	defer sourceServer.Close()
	startOracleConsumerSidecar(t, node, sourceServer.URL)
	defer stopOracleSidecar(t, node)

	node.node = startOracleConsumerNode(t, repoRoot, bin, node, appDBBackend)
	defer func() { stopNode(t, node.node) }()
	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, 7, 90*time.Second)
	waitForOracleLatestHeight(t, repoRoot, bin, node.home, node.rpcAddr, "BTC/USD", 4, 90*time.Second)

	cometClient, err := rpchttp.New(fmt.Sprintf("http://127.0.0.1:%d", node.rpcPort), "/websocket")
	require.NoError(t, err)
	grpcConn, err := grpc.NewClient(node.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = grpcConn.Close() }()
	txClient := txtypes.NewServiceClient(grpcConn)

	apiBase := fmt.Sprintf("http://127.0.0.1:%d", node.apiPort)
	jsonRPCURL := fmt.Sprintf("http://127.0.0.1:%d", node.jsonRPCPort)
	requireRESTContains(t, apiBase+"/cosmos/base/tendermint/v1beta1/node_info", "default_node_info")

	cosmosOut := runCmd(
		t, repoRoot, bin,
		"tx", "bank", "send", "moderator", evmAddress.Hex(), "1agxn",
		"--from", "moderator",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--node", node.rpcAddr,
		"--gas", "250000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	cosmosHash := parseTxHashFromSyncResponse(t, cosmosOut)
	waitForTx(t, repoRoot, bin, node.home, node.rpcAddr, cosmosHash)
	cosmosHashBytes, err := hex.DecodeString(cosmosHash)
	require.NoError(t, err)
	cosmosResult, err := cometClient.Tx(context.Background(), cosmosHashBytes, true)
	require.NoError(t, err)
	cosmosOracle := assertOracleBlockConsumerCompatibility(
		t, cometClient, txClient, apiBase, cosmosResult.Height, 2,
	)
	require.Equal(t, cosmosHashBytes, []byte(cosmosResult.Hash))

	injected, err := oracleabci.EncodeProposalTx(&oracletypes.OracleProposalPayload{Height: 999})
	require.NoError(t, err)
	broadcast, err := txClient.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: injected,
		Mode:    txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	require.NoError(t, err)
	require.NotNil(t, broadcast.GetTxResponse())
	require.NotZero(t, broadcast.GetTxResponse().Code)
	require.Contains(t, strings.ToLower(broadcast.GetTxResponse().RawLog), "reserved for consensus records")
	unconfirmed, err := cometClient.NumUnconfirmedTxs(context.Background())
	require.NoError(t, err)
	require.Zero(t, unconfirmed.Count)

	rpcClient, err := gethrpc.DialHTTP(jsonRPCURL)
	require.NoError(t, err)
	defer rpcClient.Close()
	ethClient := ethclient.NewClient(rpcClient)

	nonce, err := ethClient.PendingNonceAt(context.Background(), evmAddress)
	require.NoError(t, err)
	receiver := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	signedTx, err := gethtypes.SignNewTx(
		privateKey,
		gethtypes.LatestSignerForChainID(big.NewInt(631)),
		&gethtypes.LegacyTx{
			Nonce:    nonce,
			To:       &receiver,
			Value:    big.NewInt(1),
			Gas:      21_000,
			GasPrice: big.NewInt(1_000_000_000_000),
		},
	)
	require.NoError(t, err)
	require.NoError(t, ethClient.SendTransaction(context.Background(), signedTx))
	receipt := waitForEthereumReceipt(t, ethClient, signedTx.Hash(), 60*time.Second)
	require.Equal(t, gethtypes.ReceiptStatusSuccessful, receipt.Status)
	require.Zero(t, receipt.TransactionIndex)

	evmHeight := receipt.BlockNumber.Int64()
	evmOracle := assertOracleBlockConsumerCompatibility(t, cometClient, txClient, apiBase, evmHeight, 2)
	assertEthereumConsumerCompatibility(t, ethClient, rpcClient, signedTx, receipt)

	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, evmHeight+4, 90*time.Second)
	tailOracle := findOracleOnlyTail(t, cometClient, evmHeight+1, latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node}))
	assertOracleBlockConsumerCompatibility(t, cometClient, txClient, apiBase, tailOracle.Height, 1)

	liveLogs := oracleSoakNodeLogs(node)
	assertNoOracleDecoderErrors(t, "live node", liveLogs)
	stopNode(t, node.node)
	if requireIndexerPersistence {
		node.node = startOracleConsumerNode(t, repoRoot, bin, node, appDBBackend)
		waitForOracleSoakNodeHeight(t, repoRoot, bin, node, tailOracle.Height+2, 90*time.Second)
		restartedReceipt := waitForEthereumReceipt(t, ethClient, signedTx.Hash(), 30*time.Second)
		require.Equal(t, receipt.BlockHash, restartedReceipt.BlockHash)
		require.Equal(t, receipt.BlockNumber, restartedReceipt.BlockNumber)
		assertEthereumConsumerCompatibility(t, ethClient, rpcClient, signedTx, receipt)
		assertNoOracleDecoderErrors(t, "restarted live index", oracleSoakNodeLogs(node))
		stopNode(t, node.node)

		indexDBPath := filepath.Join(node.home, "data", "evmindexer.db")
		require.DirExists(t, indexDBPath)
		require.NoError(t, os.Rename(indexDBPath, indexDBPath+".live-backup"))
	}

	reindexOut, err := runOracleSoakCmdE(
		repoRoot,
		bin,
		2*time.Minute,
		"index-eth-tx", "forward",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--log_level", "error",
	)
	require.NoError(t, err, "offline index output:\n%s", reindexOut)
	require.True(t, outputContainsLine(reindexOut, strconv.FormatInt(tailOracle.Height, 10)), "offline index did not traverse Oracle-only tail height %d:\n%s", tailOracle.Height, reindexOut)
	if requireIndexerPersistence {
		require.True(t, outputContainsLine(reindexOut, strconv.FormatInt(evmHeight, 10)), "offline index did not rebuild EVM height %d from an empty index DB:\n%s", evmHeight, reindexOut)
	}
	assertNoOracleDecoderErrors(t, "offline reindex", reindexOut)
	t.Logf("offline index-eth-tx forward output:\n%s", reindexOut)

	node.node = startOracleConsumerNode(t, repoRoot, bin, node, appDBBackend)
	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, tailOracle.Height+2, 90*time.Second)
	assertOracleGetTx(t, txClient, tailOracle.Hash)
	if requireIndexerPersistence {
		restartedReceipt := waitForEthereumReceipt(t, ethClient, signedTx.Hash(), 30*time.Second)
		require.Equal(t, receipt.BlockHash, restartedReceipt.BlockHash)
		require.Equal(t, receipt.BlockNumber, restartedReceipt.BlockNumber)
		assertEthereumConsumerCompatibility(t, ethClient, rpcClient, signedTx, receipt)
	} else if _, err := waitForEthereumReceiptE(ethClient, signedTx.Hash(), 5*time.Second); err == nil {
		assertEthereumConsumerCompatibility(t, ethClient, rpcClient, signedTx, receipt)
	} else {
		require.Contains(t, strings.ToLower(err.Error()), "not found")
		t.Logf("separate Cosmos EVM indexer restart persistence defect reproduced after Oracle-safe offline reindex: %v", err)
	}
	assertNoOracleDecoderErrors(t, "restarted node", oracleSoakNodeLogs(node))

	t.Logf(
		"Oracle consumer compatibility passed app_db_backend=%s cosmos_height=%d cosmos_oracle_hash=%X evm_height=%d evm_hash=%s evm_oracle_hash=%X tail_height=%d tail_oracle_hash=%X moderator=%s",
		appDBBackend,
		cosmosResult.Height,
		cosmosOracle.Hash,
		evmHeight,
		signedTx.Hash(),
		evmOracle.Hash,
		tailOracle.Height,
		tailOracle.Hash,
		accounts.moderator,
	)
}

type oracleConsumerRecord struct {
	Height int64
	Tx     []byte
	Hash   []byte
}

func patchOracleConsumerGenesis(t *testing.T, home string) {
	t.Helper()

	genesisPath := filepath.Join(home, "config", "genesis.json")
	bz, err := os.ReadFile(genesisPath)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(bz, &doc))
	appState := mustJSONMap(t, doc, "app_state")
	oracleState := mustJSONMap(t, appState, "oracle")
	oracleState["params"] = map[string]any{
		"min_validators": 1,
		"min_sources":    3,
		"history_limit":  100,
	}
	oracleState["tasks"] = []any{
		map[string]any{"symbol": "BTC/USD", "value_type": 1, "enabled": true, "submission_interval": 1},
	}
	oracleState["task_schedule"] = []any{
		map[string]any{"symbol": "BTC/USD", "height": 3},
		map[string]any{"symbol": "BTC/USD", "height": 4},
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(genesisPath, out, 0o644))
}

func setOracleConsumerAppDBBackend(t *testing.T, home, backend string) {
	t.Helper()
	require.Contains(t, []string{"goleveldb", "pebbledb"}, backend)

	appTomlPath := filepath.Join(home, "config", "app.toml")
	bz, err := os.ReadFile(appTomlPath)
	require.NoError(t, err)
	content := string(bz)
	backendConfig := fmt.Sprintf(`app-db-backend = %q`, backend)
	if strings.Contains(content, `app-db-backend = ""`) {
		content = strings.Replace(content, `app-db-backend = ""`, backendConfig, 1)
	} else {
		require.Contains(t, content, backendConfig)
	}
	require.NoError(t, os.WriteFile(appTomlPath, []byte(content), 0o644))
}

func startOracleConsumerSidecar(t *testing.T, node *oracleSoakNode, sourceURL string) {
	t.Helper()

	cfg := oracle.Config{
		Socket:           node.oracleSocket,
		RequestTimeout:   "2s",
		SourceTimeout:    "500ms",
		NodeGRPC:         node.grpcAddr,
		NodeQueryTimeout: "2s",
		Sources: []oracle.SourceConfig{
			{Name: "btc-a", Symbol: "BTC/USD", ValueType: "numeric", URL: sourceURL + "/price?symbol={symbol}&price=101.0", ResponsePath: "data.price"},
			{Name: "btc-b", Symbol: "BTC/USD", ValueType: "numeric", URL: sourceURL + "/price?symbol={symbol}&price=102.0", ResponsePath: "data.price"},
			{Name: "btc-c", Symbol: "BTC/USD", ValueType: "numeric", URL: sourceURL + "/price?symbol={symbol}&price=103.0", ResponsePath: "data.price"},
		},
	}
	sidecar, err := oracle.NewSidecar(cfg, []*oracletypes.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sidecar.Run(ctx) }()
	node.sidecar = &oracleSoakSidecar{cancel: cancel, done: done}
	waitForOracleSocket(t, node.oracleSocket, done, 5*time.Second)
}

func startOracleConsumerNode(t *testing.T, repoRoot, bin string, node *oracleSoakNode, appDBBackend string) *runningNode {
	t.Helper()

	return startNodeWithOptions(
		t,
		repoRoot,
		bin,
		node.home,
		node.rpcPort,
		node.p2pPort,
		node.pprofPort,
		node.grpcPort,
		node.jsonRPCPort,
		node.jsonWSRPCPort,
		[]string{
			"--app-db-backend", appDBBackend,
			"--api.enable", "true",
			"--json-rpc.enable", "true",
			"--json-rpc.enable-indexer", "true",
			"--json-rpc.api", "eth,net,web3,debug",
			"--json-rpc.allow-insecure-unlock", "false",
			"--state-sync.snapshot-interval", "0",
		},
		nil,
	)
}

func assertOracleBlockConsumerCompatibility(
	t *testing.T,
	cometClient *rpchttp.HTTP,
	txClient txtypes.ServiceClient,
	apiBase string,
	height int64,
	wantRawCount int,
) oracleConsumerRecord {
	t.Helper()
	ctx := context.Background()

	block, err := cometClient.Block(ctx, &height)
	require.NoError(t, err)
	require.Len(t, block.Block.Txs, wantRawCount)
	oracleTx := block.Block.Txs[0]
	payload, candidate, err := oracleabci.DecodeProposalTx(oracleTx)
	require.NoError(t, err)
	require.True(t, candidate)
	require.Equal(t, height, payload.GetHeight())
	oracleHash := cmttypes.Tx(oracleTx).Hash()

	blockResults, err := cometClient.BlockResults(ctx, &height)
	require.NoError(t, err)
	require.Len(t, blockResults.TxsResults, wantRawCount)
	require.Zero(t, blockResults.TxsResults[0].Code)
	cometTx, err := cometClient.Tx(ctx, oracleHash, true)
	require.NoError(t, err)
	require.Equal(t, uint32(0), cometTx.Index)
	require.Equal(t, []byte(oracleTx), []byte(cometTx.Tx))
	search, err := cometClient.TxSearch(ctx, fmt.Sprintf("tx.height=%d", height), true, nil, nil, "asc")
	require.NoError(t, err)
	require.Equal(t, wantRawCount, search.TotalCount)
	require.NotEmpty(t, search.Txs)
	require.Equal(t, []byte(oracleHash), []byte(search.Txs[0].Hash))

	full, err := txClient.GetBlockWithTxs(ctx, &txtypes.GetBlockWithTxsRequest{
		Height: height,
		Pagination: &query.PageRequest{
			Limit:      uint64(wantRawCount),
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, full.GetBlock().Data.Txs, wantRawCount)
	require.Len(t, full.GetTxs(), wantRawCount)
	require.Equal(t, uint64(wantRawCount), full.GetPagination().GetTotal())
	require.Len(t, full.GetTxs()[0].GetBody().GetExtensionOptions(), 1)
	require.Equal(t, "/guru.oracle.v1.OracleProposalPayload", full.GetTxs()[0].GetBody().GetExtensionOptions()[0].GetTypeUrl())
	for offset := 0; offset < wantRawCount; offset++ {
		page, err := txClient.GetBlockWithTxs(ctx, &txtypes.GetBlockWithTxsRequest{
			Height: height,
			Pagination: &query.PageRequest{
				Offset:     uint64(offset),
				Limit:      1,
				CountTotal: true,
			},
		})
		require.NoError(t, err)
		require.Len(t, page.GetTxs(), 1)
		require.Equal(t, uint64(wantRawCount), page.GetPagination().GetTotal())
	}

	assertOracleGetTx(t, txClient, oracleHash)
	eventTxs, err := txClient.GetTxsEvent(ctx, &txtypes.GetTxsEventRequest{
		Query:   fmt.Sprintf("tx.height=%d", height),
		Limit:   uint64(wantRawCount + 1),
		OrderBy: txtypes.OrderBy_ORDER_BY_ASC,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(wantRawCount), eventTxs.GetTotal())
	require.Len(t, eventTxs.GetTxs(), wantRawCount)
	require.Len(t, eventTxs.GetTxs()[0].GetBody().GetExtensionOptions(), 1)

	hashHex := strings.ToUpper(hex.EncodeToString(oracleHash))
	requireRESTContains(t, apiBase+"/cosmos/tx/v1beta1/txs/"+hashHex, "guru.oracle.v1.OracleProposalPayload")
	blockURL := fmt.Sprintf("%s/cosmos/tx/v1beta1/txs/block/%d?pagination.limit=1&pagination.offset=0&pagination.count_total=true", apiBase, height)
	requireRESTContains(t, blockURL, "guru.oracle.v1.OracleProposalPayload")
	eventQuery := url.Values{}
	eventQuery.Set("query", fmt.Sprintf("tx.height=%d", height))
	eventQuery.Set("limit", strconv.Itoa(wantRawCount+1))
	requireRESTContains(t, apiBase+"/cosmos/tx/v1beta1/txs?"+eventQuery.Encode(), "guru.oracle.v1.OracleProposalPayload")

	return oracleConsumerRecord{Height: height, Tx: append([]byte(nil), oracleTx...), Hash: append([]byte(nil), oracleHash...)}
}

func assertOracleGetTx(t *testing.T, txClient txtypes.ServiceClient, hash []byte) {
	t.Helper()

	request := &txtypes.GetTxRequest{Hash: strings.ToUpper(hex.EncodeToString(hash))}
	deadline := time.Now().Add(20 * time.Second)
	var (
		resp *txtypes.GetTxResponse
		err  error
	)
	for time.Now().Before(deadline) {
		resp, err = txClient.GetTx(context.Background(), request)
		if err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	require.NoError(t, err)
	require.NotNil(t, resp.GetTx())
	require.Len(t, resp.GetTx().GetBody().GetExtensionOptions(), 1)
	require.Equal(t, "/guru.oracle.v1.OracleProposalPayload", resp.GetTx().GetBody().GetExtensionOptions()[0].GetTypeUrl())
	require.Equal(t, int64(0), resp.GetTxResponse().GasWanted)
	require.Equal(t, int64(0), resp.GetTxResponse().GasUsed)
}

func waitForEthereumReceipt(t *testing.T, client *ethclient.Client, hash common.Hash, timeout time.Duration) *gethtypes.Receipt {
	t.Helper()
	receipt, err := waitForEthereumReceiptE(client, hash, timeout)
	require.NoError(t, err)
	return receipt
}

func waitForEthereumReceiptE(client *ethclient.Client, hash common.Hash, timeout time.Duration) (*gethtypes.Receipt, error) {

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		receipt, err := client.TransactionReceipt(context.Background(), hash)
		if err == nil {
			return receipt, nil
		}
		lastErr = err
		time.Sleep(300 * time.Millisecond)
	}
	return nil, fmt.Errorf("Ethereum receipt %s not available within %s: %w", hash, timeout, lastErr)
}

func assertEthereumConsumerCompatibility(
	t *testing.T,
	client *ethclient.Client,
	rpcClient *gethrpc.Client,
	tx *gethtypes.Transaction,
	receipt *gethtypes.Receipt,
) {
	t.Helper()
	ctx := context.Background()

	byNumber, err := client.BlockByNumber(ctx, receipt.BlockNumber)
	require.NoError(t, err)
	require.Len(t, byNumber.Transactions(), 1)
	require.Equal(t, tx.Hash(), byNumber.Transactions()[0].Hash())
	byHash, err := client.BlockByHash(ctx, receipt.BlockHash)
	require.NoError(t, err)
	require.Equal(t, receipt.BlockNumber, byHash.Number())
	require.Len(t, byHash.Transactions(), 1)
	require.Equal(t, tx.Hash(), byHash.Transactions()[0].Hash())
	var rawBlock map[string]any
	require.NoError(t, rpcClient.CallContext(ctx, &rawBlock, "eth_getBlockByNumber", hexutil.EncodeBig(receipt.BlockNumber), true))
	rawBlockHash, ok := rawBlock["hash"].(string)
	require.True(t, ok)
	require.Equal(t, strings.ToLower(receipt.BlockHash.Hex()), strings.ToLower(rawBlockHash))
	count, err := client.TransactionCount(ctx, receipt.BlockHash)
	require.NoError(t, err)
	require.Equal(t, uint(1), count)
	readTx, pending, err := client.TransactionByHash(ctx, tx.Hash())
	require.NoError(t, err)
	require.False(t, pending)
	require.Equal(t, tx.Hash(), readTx.Hash())
	readReceipt, err := client.TransactionReceipt(ctx, tx.Hash())
	require.NoError(t, err)
	require.Equal(t, receipt.BlockHash, readReceipt.BlockHash)
	require.Zero(t, readReceipt.TransactionIndex)
	logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: receipt.BlockNumber, ToBlock: receipt.BlockNumber})
	require.NoError(t, err)
	require.Empty(t, logs)
	feeHistory, err := client.FeeHistory(ctx, 1, receipt.BlockNumber, nil)
	require.NoError(t, err)
	require.NotNil(t, feeHistory)

	var traceTx json.RawMessage
	require.NoError(t, rpcClient.CallContext(ctx, &traceTx, "debug_traceTransaction", tx.Hash(), nil))
	require.NotEmpty(t, traceTx)
	var traceBlock json.RawMessage
	require.NoError(t, rpcClient.CallContext(ctx, &traceBlock, "debug_traceBlockByNumber", hexutil.EncodeBig(receipt.BlockNumber), nil))
	require.NotEmpty(t, traceBlock)
}

func findOracleOnlyTail(t *testing.T, client *rpchttp.HTTP, firstHeight, lastHeight int64) oracleConsumerRecord {
	t.Helper()

	for height := lastHeight; height >= firstHeight; height-- {
		block, err := client.Block(context.Background(), &height)
		require.NoError(t, err)
		if len(block.Block.Txs) != 1 {
			continue
		}
		if _, candidate, err := oracleabci.DecodeProposalTx(block.Block.Txs[0]); candidate && err == nil {
			oracleTx := block.Block.Txs[0]
			return oracleConsumerRecord{
				Height: height,
				Tx:     append([]byte(nil), oracleTx...),
				Hash:   append([]byte(nil), cmttypes.Tx(oracleTx).Hash()...),
			}
		}
	}
	t.Fatalf("no Oracle-only block found in [%d,%d]", firstHeight, lastHeight)
	return oracleConsumerRecord{}
}

func requireRESTContains(t *testing.T, endpoint, marker string) {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			last = fmt.Sprintf("status=%d body=%s", resp.StatusCode, body)
			if readErr == nil && resp.StatusCode == http.StatusOK && strings.Contains(string(body), marker) {
				return
			}
		} else {
			last = err.Error()
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("REST endpoint %s did not return marker %q: %s", endpoint, marker, last)
}

func assertNoOracleDecoderErrors(t *testing.T, source, output string) {
	t.Helper()

	lower := strings.ToLower(output)
	for _, marker := range []string{
		"fail to decode tx",
		"failed to decode transaction",
		"failed to decode tx",
		"illegal wiretype",
		"wire type",
		"unregistered oracleproposalpayload",
		"no concrete type registered for type url /guru.oracle.v1.oracleproposalpayload",
	} {
		if strings.Contains(lower, marker) {
			t.Fatalf("%s contains Oracle decoder error marker %q:\n%s", source, marker, output)
		}
	}
}

func outputContainsLine(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
