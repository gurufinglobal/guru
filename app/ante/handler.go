package ante

import (
	evmante "github.com/cosmos/evm/ante"
	cosmosante "github.com/cosmos/evm/ante/cosmos"
	evmanteevm "github.com/cosmos/evm/ante/evm"
	antetypes "github.com/cosmos/evm/ante/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibcante "github.com/cosmos/ibc-go/v10/modules/core/ante"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

var (
	ethereumExtensionTypeURL = sdk.MsgTypeURL(&evmtypes.ExtensionOptionsEthereumTx{})
	dynamicFeeExtensionURL   = sdk.MsgTypeURL(&antetypes.ExtensionOptionDynamicFeeTx{})
)

// NewAnteHandler preserves the upstream Cosmos EVM v0.6.1 Ethereum path and
// routes Cosmos transactions through the application-local standard MsgSend
// gas extension.
func NewAnteHandler(options evmante.HandlerOptions) sdk.AnteHandler {
	upstreamHandler := evmante.NewAnteHandler(options)

	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if tx == nil {
			return upstreamHandler(ctx, tx, simulate)
		}

		if extensionTx, ok := tx.(authante.HasExtensionOptionsTx); ok {
			extensions := extensionTx.GetExtensionOptions()
			if len(extensions) > 0 {
				switch extensions[0].GetTypeUrl() {
				case ethereumExtensionTypeURL:
					return upstreamHandler(ctx, tx, simulate)
				case dynamicFeeExtensionURL:
					return newCosmosAnteHandler(ctx, options)(ctx, tx, simulate)
				default:
					return upstreamHandler(ctx, tx, simulate)
				}
			}
		}

		return newCosmosAnteHandler(ctx, options)(ctx, tx, simulate)
	}
}

// newCosmosAnteHandler mirrors the Cosmos EVM v0.6.1 Cosmos ante chain. Fee
// market parameters are read from the transaction context on every invocation.
func newCosmosAnteHandler(ctx sdk.Context, options evmante.HandlerOptions) sdk.AnteHandler {
	feemarketParams := options.FeeMarketKeeper.GetParams(ctx)

	var txFeeChecker authante.TxFeeChecker
	if options.DynamicFeeChecker {
		txFeeChecker = newStandardMsgSendDynamicFeeChecker(&feemarketParams)
	} else {
		txFeeChecker = newStandardMsgSendValidatorFeeChecker()
	}

	return sdk.ChainAnteDecorators(
		cosmosante.NewRejectMessagesDecorator(),
		cosmosante.NewAuthzLimiterDecorator(
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
		),
		authante.NewSetUpContextDecorator(),
		NewStandardMsgSendGasDecorator(
			options.AccountKeeper,
			feemarketParams.MinGasMultiplier,
			options.MaxTxGasWanted,
		),
		authante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		authante.NewValidateBasicDecorator(),
		authante.NewTxTimeoutHeightDecorator(),
		authante.NewValidateMemoDecorator(options.AccountKeeper),
		newStandardMsgSendMinGasPriceDecorator(&feemarketParams),
		authante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		authante.NewDeductFeeDecorator(
			options.AccountKeeper,
			options.BankKeeper,
			options.FeegrantKeeper,
			txFeeChecker,
		),
		authante.NewSetPubKeyDecorator(options.AccountKeeper),
		authante.NewValidateSigCountDecorator(options.AccountKeeper),
		authante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		authante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		authante.NewIncrementSequenceDecorator(options.AccountKeeper),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
		evmanteevm.NewGasWantedDecorator(
			options.EvmKeeper,
			options.FeeMarketKeeper,
			&feemarketParams,
		),
	)
}
