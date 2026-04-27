package app

import (
	"errors"

	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	evmmempool "github.com/cosmos/evm/mempool"
	evmserver "github.com/cosmos/evm/server"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

func (app *App) configureEVMMempool(appOpts servertypes.AppOptions, logger log.Logger) error {
	if evmtypes.GetChainConfig() == nil {
		logger.Debug("evm chain config is not set, skipping mempool configuration")
		return nil
	}

	anteHandler := app.anteHandler
	if anteHandler == nil {
		return errors.New("ante handler must be configured before EVM mempool")
	}

	txEncoder := evmmempool.NewTxEncoder(app.txConfig)
	evmRechecker := evmmempool.NewTxRechecker(anteHandler, txEncoder)
	cosmosRechecker := evmmempool.NewTxRechecker(anteHandler, txEncoder)

	mempoolCfg := evmserver.ResolveMempoolConfig(anteHandler, appOpts, logger)
	cosmosPoolMaxTx := evmserver.GetCosmosPoolMaxTx(appOpts, logger)
	if cosmosPoolMaxTx < 0 {
		logger.Debug("evm mempool is disabled, skipping configuration")
		return nil
	}

	evmMempool := evmmempool.NewMempool(
		app.CreateQueryContext,
		logger,
		app.EVMKeeper,
		app.FeeMarketKeeper,
		app.txConfig,
		evmRechecker,
		cosmosRechecker,
		mempoolCfg,
		cosmosPoolMaxTx,
	)

	proposalHandler := baseapp.NewDefaultProposalHandler(
		evmMempool,
		NewNoCheckProposalTxVerifier(app.BaseApp),
	)
	proposalHandler.SetSignerExtractionAdapter(
		evmmempool.NewEthSignerExtractionAdapter(
			sdkmempool.NewDefaultSignerExtractionAdapter(),
		),
	)

	txDecoder := app.txConfig.TxDecoder()
	app.SetPrepareProposal(proposalHandler.PrepareProposalHandler())
	app.SetProcessProposal(proposalHandler.ProcessProposalHandler())
	app.SetInsertTxHandler(evmMempool.NewInsertTxHandler(txDecoder))
	app.SetReapTxsHandler(evmMempool.NewReapTxsHandler())
	app.SetCheckTxHandler(evmMempool.NewCheckTxHandler(
		txDecoder,
		evmserver.GetMempoolCheckTxTimeout(appOpts, logger),
	))
	app.SetMempool(evmMempool)
	app.EVMMempool = evmMempool

	return nil
}
