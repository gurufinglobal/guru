package app

import (
	"fmt"

	evmante "github.com/cosmos/evm/ante"
	anteevmtypes "github.com/cosmos/evm/ante/types"
	"github.com/ethereum/go-ethereum/common"
)

func (app *App) configureAnteHandler(maxTxGasWanted uint64) error {
	options := evmante.HandlerOptions{
		Cdc:                    app.AppCodec(),
		AccountKeeper:          app.AccountKeeper,
		BankKeeper:             app.BankKeeper,
		IBCKeeper:              app.IBCKeeper,
		FeeMarketKeeper:        app.FeeMarketKeeper,
		EvmKeeper:              app.EVMKeeper,
		FeegrantKeeper:         app.FeeGrantKeeper,
		ExtensionOptionChecker: anteevmtypes.HasDynamicFeeExtensionOption,
		SignModeHandler:        app.TxConfig().SignModeHandler(),
		SigGasConsumer:         evmante.SigVerificationGasConsumer,
		MaxTxGasWanted:         maxTxGasWanted,
		DynamicFeeChecker:      true,
		PendingTxListener:      func(common.Hash) {},
	}
	if err := options.Validate(); err != nil {
		return fmt.Errorf("validate ante handler options: %w", err)
	}
	app.anteHandler = evmante.NewAnteHandler(options)
	app.SetAnteHandler(app.anteHandler)
	return nil
}
