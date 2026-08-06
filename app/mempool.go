package app

import (
	"errors"

	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	evmmempool "github.com/cosmos/evm/mempool"
	evmserver "github.com/cosmos/evm/server"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

func (app *App) configureEVMMempool(appOpts servertypes.AppOptions, logger log.Logger) error {
	if evmtypes.GetChainConfig() == nil {
		logger.Debug("evm chain config is not set, using default proposal handler with oracle payload wrapper")
		app.configureNoOpOracleProposalHandler()
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
		logger.Debug("evm mempool is disabled, using default proposal handler with oracle payload wrapper")
		app.configureNoOpOracleProposalHandler()
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
	proposalHandler.SetTxSelector(newStandardMsgSendTxSelector())
	proposalHandler.SetSignerExtractionAdapter(
		evmmempool.NewEthSignerExtractionAdapter(
			sdkmempool.NewDefaultSignerExtractionAdapter(),
		),
	)
	app.configureOracleProposalHandler(
		proposalHandler.PrepareProposalHandler(),
		app.standardMsgSendProcessProposal(proposalHandler.ProcessProposalHandler()),
	)

	txDecoder := app.txConfig.TxDecoder()
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

func (app *App) configureNoOpOracleProposalHandler() {
	proposalHandler := baseapp.NewDefaultProposalHandler(
		sdkmempool.NoOpMempool{},
		NewNoCheckProposalTxVerifier(app.BaseApp),
	)
	app.configureOracleProposalHandler(
		proposalHandler.PrepareProposalHandler(),
		app.standardMsgSendProcessProposal(proposalHandler.ProcessProposalHandler()),
	)
}

func (app *App) configureOracleProposalHandler(
	prepareProposalHandler sdk.PrepareProposalHandler,
	processProposalHandler sdk.ProcessProposalHandler,
) {
	oracleAggregator := oracleabci.NewAggregator(app.OracleKeeper, app.StakingKeeper)
	oracleProposalHandler := oracleabci.NewProposalHandler(
		oracleAggregator,
		prepareProposalHandler,
		processProposalHandler,
	)

	app.SetPrepareProposal(oracleProposalHandler.PrepareProposal)
	app.SetProcessProposal(oracleProposalHandler.ProcessProposal)
	app.OracleProposalHandler = &oracleProposalHandler
}
