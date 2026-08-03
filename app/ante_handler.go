package app

import (
	"github.com/cosmos/cosmos-sdk/client"
	appante "github.com/gurufinglobal/guru/v3/app/ante"

	evmante "github.com/cosmos/evm/ante"
	antetypes "github.com/cosmos/evm/ante/types"
	"github.com/ethereum/go-ethereum/common"
)

func (app *App) onPendingTx(hash common.Hash) {
	for _, listener := range app.pendingTxListeners {
		listener(hash)
	}
}

func (app *App) setAnteHandler(txConfig client.TxConfig, maxGasWanted uint64) error {
	options := appante.HandlerOptions{
		EVMOptions: evmante.HandlerOptions{
			Cdc:                    app.appCodec,
			AccountKeeper:          app.AccountKeeper,
			BankKeeper:             app.BankKeeper,
			ExtensionOptionChecker: antetypes.HasDynamicFeeExtensionOption,
			EvmKeeper:              app.EVMKeeper,
			FeegrantKeeper:         app.FeeGrantKeeper,
			IBCKeeper:              app.IBCKeeper,
			FeeMarketKeeper:        app.FeeMarketKeeper,
			SignModeHandler:        txConfig.SignModeHandler(),
			SigGasConsumer:         appante.SigVerificationGasConsumer,
			MaxTxGasWanted:         maxGasWanted,
			DynamicFeeChecker:      true,
			PendingTxListener:      app.onPendingTx,
		},
		FeePolicyKeeper: app.FeePolicyKeeper,
	}

	anteHandler, err := appante.NewAnteHandler(options)
	if err != nil {
		return err
	}
	anteHandler = appante.WrapAnteHandlerWithSelfBondCheck(anteHandler, app.CustomStakingKeeper)
	anteHandler = appante.WrapAnteHandlerWithLegacyGovBlock(anteHandler)
	anteHandler = appante.WrapAnteHandlerWithOracleProposalOptionBlock(anteHandler)

	app.anteHandler = anteHandler
	app.SetAnteHandler(anteHandler)
	return nil
}
