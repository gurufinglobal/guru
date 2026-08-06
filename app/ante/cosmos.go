package ante

import (
	cosmosante "github.com/cosmos/evm/ante/cosmos"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibcante "github.com/cosmos/ibc-go/v11/modules/core/ante"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

// newCosmosAnteHandler mirrors the Cosmos EVM v0.7 Cosmos ante chain. The fee
// market parameters are read in the transaction context, rather than captured
// when the application constructs its ante handler.
func newCosmosAnteHandler(
	ctx sdk.Context,
	options HandlerOptions,
	feeCollector CosmosFeeCollector,
) sdk.AnteHandler {
	evmOptions := options.EVMOptions
	feemarketParams := evmOptions.FeeMarketKeeper.GetParams(ctx)

	var txFeeChecker TxFeeChecker
	if evmOptions.DynamicFeeChecker {
		txFeeChecker = NewDynamicFeeChecker(&feemarketParams)
	}

	return sdk.ChainAnteDecorators(
		cosmosante.NewRejectMessagesDecorator(),
		cosmosante.NewAuthzLimiterDecorator(
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
		),
		authante.NewSetUpContextDecorator(),
		NewStandardMsgSendGasDecorator(evmOptions.AccountKeeper),
		authante.NewExtensionOptionsDecorator(evmOptions.ExtensionOptionChecker),
		authante.NewValidateBasicDecorator(),
		authante.NewTxTimeoutHeightDecorator(),
		authante.NewValidateMemoDecorator(evmOptions.AccountKeeper),
		cosmosante.NewMinGasPriceDecorator(&feemarketParams),
		authante.NewConsumeGasForTxSizeDecorator(evmOptions.AccountKeeper),
		NewDeductFeeDecorator(
			evmOptions.AccountKeeper,
			feeCollector,
			evmOptions.FeegrantKeeper,
			txFeeChecker,
			options.FeePolicyKeeper,
		),
		authante.NewSetPubKeyDecorator(evmOptions.AccountKeeper),
		authante.NewValidateSigCountDecorator(evmOptions.AccountKeeper),
		authante.NewSigGasConsumeDecorator(evmOptions.AccountKeeper, evmOptions.SigGasConsumer),
		authante.NewSigVerificationDecorator(evmOptions.AccountKeeper, evmOptions.SignModeHandler),
		authante.NewIncrementSequenceDecorator(evmOptions.AccountKeeper),
		ibcante.NewRedundantRelayDecorator(evmOptions.IBCKeeper),
	)
}
