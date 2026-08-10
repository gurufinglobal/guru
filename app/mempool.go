package app

import (
	"errors"
	"fmt"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	evmconfig "github.com/cosmos/evm/config"
	evmmempool "github.com/cosmos/evm/mempool"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

func (app *App) configureEVMMempool(appOpts servertypes.AppOptions, logger log.Logger) error {
	anteHandler := app.anteHandler
	if anteHandler == nil {
		return errors.New("ante handler must be configured before EVM mempool")
	}

	mempoolCfg := &evmmempool.EVMMempoolConfig{
		LegacyPoolConfig: evmconfig.GetLegacyPoolConfig(appOpts, logger),
		AnteHandler:      anteHandler,
		BroadCastTxFn:    app.broadcastEVMTransactions,
		BlockGasLimit:    evmconfig.GetBlockGasLimit(appOpts, logger),
		MinTip:           evmconfig.GetMinTip(appOpts, logger),
	}
	evmMempool := evmmempool.NewExperimentalEVMMempool(
		app.CreateQueryContext,
		logger,
		app.EVMKeeper,
		app.FeeMarketKeeper,
		app.txConfig,
		client.Context{},
		mempoolCfg,
		evmconfig.GetCosmosPoolMaxTx(appOpts, logger),
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
	app.configureOracleProposalHandler(proposalHandler)

	app.SetCheckTxHandler(evmmempool.NewCheckTxHandler(evmMempool))
	app.SetMempool(evmMempool)
	app.EVMMempool = evmMempool

	return nil
}

func (app *App) broadcastEVMTransactions(ethTxs []*ethtypes.Transaction) error {
	for _, ethTx := range ethTxs {
		msg := &evmtypes.MsgEthereumTx{}
		msg.FromEthereumTx(ethTx)

		txBuilder := app.txConfig.NewTxBuilder()
		if err := txBuilder.SetMsgs(msg); err != nil {
			return fmt.Errorf("set promoted EVM message: %w", err)
		}
		txBytes, err := app.txConfig.TxEncoder()(txBuilder.GetTx())
		if err != nil {
			return fmt.Errorf("encode promoted EVM transaction: %w", err)
		}
		res, err := app.clientCtx.BroadcastTxSync(txBytes)
		if err != nil {
			return fmt.Errorf("broadcast promoted EVM transaction %s: %w", ethTx.Hash().Hex(), err)
		}
		if res.Code != 0 {
			return fmt.Errorf(
				"promoted EVM transaction %s rejected: code=%d log=%s",
				ethTx.Hash().Hex(),
				res.Code,
				res.RawLog,
			)
		}
	}
	return nil
}

func (app *App) configureOracleProposalHandler(proposalHandler *baseapp.DefaultProposalHandler) {
	oracleAggregator := oracleabci.NewAggregator(app.OracleKeeper, app.StakingKeeper)
	oracleProposalHandler := oracleabci.NewProposalHandler(
		oracleAggregator,
		proposalHandler.PrepareProposalHandler(),
		proposalHandler.ProcessProposalHandler(),
	)

	app.SetPrepareProposal(oracleProposalHandler.PrepareProposal)
	app.SetProcessProposal(oracleProposalHandler.ProcessProposal)
	app.OracleProposalHandler = &oracleProposalHandler
}
