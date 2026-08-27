package mempool_test

import (
	"context"
	"sync"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtconfig "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/log"
	cmtmempool "github.com/cometbft/cometbft/mempool"
	"github.com/cometbft/cometbft/proxy"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestCListMempoolRecheckEvictionAndFreshInstance(t *testing.T) {
	application := newMinimumGasPriceApplication(10)
	mempool := newCometMempool(t, application)
	lowFeeTx := cmttypes.Tx{10, 1}
	highFeeTx := cmttypes.Tx{20, 2}

	require.True(t, submitTx(t, mempool, lowFeeTx).IsOK())
	require.True(t, submitTx(t, mempool, highFeeTx).IsOK())
	require.Equal(t, cmttypes.Txs{lowFeeTx, highFeeTx}, mempool.ReapMaxTxs(-1))

	// A threshold increase is observed by CheckTxType_Recheck on Update.
	// The transaction below the new threshold is evicted while the still-valid
	// transaction retains its FIFO position.
	application.setMinimumGasPrice(15)
	updateMempool(t, mempool, 1)
	require.Equal(t, cmttypes.Txs{highFeeTx}, mempool.ReapMaxTxs(-1))

	// Lowering MGP does not recreate an evicted transaction. Because invalid
	// transactions are not retained in the cache, an explicit resubmission is
	// checked as New and can become eligible again.
	application.setMinimumGasPrice(5)
	require.Equal(t, cmttypes.Txs{highFeeTx}, mempool.ReapMaxTxs(-1))
	require.True(t, submitTx(t, mempool, lowFeeTx).IsOK())
	require.Equal(t, cmttypes.Txs{highFeeTx, lowFeeTx}, mempool.ReapMaxTxs(-1))

	// A fresh in-memory mempool starts empty. A resubmitted transaction is
	// evaluated against the application's current threshold.
	restartedMempool := newCometMempool(t, application)
	require.Empty(t, restartedMempool.ReapMaxTxs(-1))
	require.True(t, submitTx(t, restartedMempool, lowFeeTx).IsOK())
	require.Equal(t, cmttypes.Txs{lowFeeTx}, restartedMempool.ReapMaxTxs(-1))

	require.Equal(t, []abci.CheckTxType{
		abci.CheckTxType_New,
		abci.CheckTxType_New,
		abci.CheckTxType_Recheck,
		abci.CheckTxType_Recheck,
		abci.CheckTxType_New,
		abci.CheckTxType_New,
	}, application.checkTypes())
}

type minimumGasPriceApplication struct {
	abci.BaseApplication

	mutex           sync.Mutex
	minimumGasPrice byte
	requests        []abci.CheckTxType
}

func newMinimumGasPriceApplication(minimumGasPrice byte) *minimumGasPriceApplication {
	return &minimumGasPriceApplication{minimumGasPrice: minimumGasPrice}
}

func (application *minimumGasPriceApplication) CheckTx(
	_ context.Context,
	request *abci.RequestCheckTx,
) (*abci.ResponseCheckTx, error) {
	application.mutex.Lock()
	defer application.mutex.Unlock()
	application.requests = append(application.requests, request.Type)

	if len(request.Tx) < 2 {
		return &abci.ResponseCheckTx{Code: 1, Log: "malformed test transaction"}, nil
	}
	if request.Tx[0] < application.minimumGasPrice {
		return &abci.ResponseCheckTx{Code: 1, Log: "gas price below minimum"}, nil
	}
	return &abci.ResponseCheckTx{Code: abci.CodeTypeOK}, nil
}

func (application *minimumGasPriceApplication) setMinimumGasPrice(value byte) {
	application.mutex.Lock()
	defer application.mutex.Unlock()
	application.minimumGasPrice = value
}

func (application *minimumGasPriceApplication) checkTypes() []abci.CheckTxType {
	application.mutex.Lock()
	defer application.mutex.Unlock()
	return append([]abci.CheckTxType(nil), application.requests...)
}

func newCometMempool(t *testing.T, application abci.Application) *cmtmempool.CListMempool {
	t.Helper()
	creator := proxy.NewLocalClientCreator(application)
	client, err := creator.NewABCIClient()
	require.NoError(t, err)
	client.SetLogger(log.NewNopLogger())
	require.NoError(t, client.Start())
	t.Cleanup(func() {
		if client.IsRunning() {
			require.NoError(t, client.Stop())
		}
	})

	config := cmtconfig.DefaultMempoolConfig()
	config.RootDir = t.TempDir()
	config.Recheck = true
	config.RecheckTimeout = time.Second
	config.KeepInvalidTxsInCache = false
	return cmtmempool.NewCListMempool(config, client, 0)
}

func submitTx(
	t *testing.T,
	mempool *cmtmempool.CListMempool,
	tx cmttypes.Tx,
) *abci.ResponseCheckTx {
	t.Helper()
	var response *abci.ResponseCheckTx
	require.NoError(t, mempool.CheckTx(tx, func(result *abci.ResponseCheckTx) {
		response = result
	}, cmtmempool.TxInfo{}))
	require.NotNil(t, response)
	return response
}

func updateMempool(t *testing.T, mempool *cmtmempool.CListMempool, height int64) {
	t.Helper()
	mempool.Lock()
	err := mempool.Update(height, nil, nil, nil, nil)
	mempool.Unlock()
	require.NoError(t, err)
}
