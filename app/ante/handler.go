package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	evmante "github.com/cosmos/evm/ante"
	cosmosante "github.com/cosmos/evm/ante/cosmos"
	evmanteevm "github.com/cosmos/evm/ante/evm"
	anteinterfaces "github.com/cosmos/evm/ante/interfaces"
	antetypes "github.com/cosmos/evm/ante/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibcante "github.com/cosmos/ibc-go/v10/modules/core/ante"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

var (
	ethereumExtensionTypeURL = sdk.MsgTypeURL(&evmtypes.ExtensionOptionsEthereumTx{})
	dynamicFeeExtensionURL   = sdk.MsgTypeURL(&antetypes.ExtensionOptionDynamicFeeTx{})
)

// ProspectiveFeeMarketKeeper extends the upstream ante keeper contract with
// the deterministic EIP-1559 calculation used by the fee market BeginBlocker.
type ProspectiveFeeMarketKeeper interface {
	anteinterfaces.FeeMarketKeeper
	CalculateBaseFee(ctx sdk.Context) sdkmath.LegacyDec
}

// prospectiveFeeMarketParamsAdapter makes proposal-time ante verification use
// the same target-height BaseFee that FinalizeBlock will install in BeginBlock.
// CheckTx, ReCheckTx, Simulate, and FinalizeBlock retain the keeper's stored
// parameters exactly.
type prospectiveFeeMarketParamsAdapter struct {
	ProspectiveFeeMarketKeeper
}

// NewProspectiveFeeMarketParamsAdapter adapts the application fee market
// keeper without changing the upstream HandlerOptions contract.
func NewProspectiveFeeMarketParamsAdapter(
	keeper ProspectiveFeeMarketKeeper,
) anteinterfaces.FeeMarketKeeper {
	return prospectiveFeeMarketParamsAdapter{ProspectiveFeeMarketKeeper: keeper}
}

func (a prospectiveFeeMarketParamsAdapter) GetParams(ctx sdk.Context) feemarkettypes.Params {
	params := a.ProspectiveFeeMarketKeeper.GetParams(ctx)
	switch ctx.ExecMode() {
	case sdk.ExecModePrepareProposal, sdk.ExecModeProcessProposal:
		prospectiveBaseFee := a.ProspectiveFeeMarketKeeper.CalculateBaseFee(ctx)
		// FeeMarket BeginBlock only stores a newly calculated BaseFee when the
		// calculation is non-nil. Mirror that lifecycle boundary so proposal ante
		// never replaces the stored value with an uninitialized decimal.
		if !prospectiveBaseFee.IsNil() {
			params.BaseFee = prospectiveBaseFee
		}
	}
	return params
}

// NewAnteHandler preserves the upstream Cosmos EVM v0.6.2 Ethereum path and
// routes Cosmos transactions through the application-local fee policy and
// standard MsgSend gas extension.
func NewAnteHandler(options evmante.HandlerOptions) sdk.AnteHandler {
	upstreamHandler := evmante.NewAnteHandler(options)
	classifier := NewStandardMsgSendClassifier(options.AccountKeeper)

	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		classification, fixed, err := classifier.classify(ctx, tx, simulate)
		if err != nil {
			return ctx, err
		}
		if fixed {
			fixedCtx := ctx.WithValue(standardMsgSendContextKey{}, &classification)
			return newCosmosAnteHandler(fixedCtx, options)(fixedCtx, tx, simulate)
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

// newCosmosAnteHandler mirrors the Cosmos EVM v0.6.2 Cosmos ante chain. Fee
// market parameters are read from the transaction context on every invocation.
func newCosmosAnteHandler(ctx sdk.Context, options evmante.HandlerOptions) sdk.AnteHandler {
	feemarketParams := options.FeeMarketKeeper.GetParams(ctx)

	var txFeeChecker authante.TxFeeChecker
	if options.DynamicFeeChecker {
		txFeeChecker = newCosmosDynamicFeeChecker(&feemarketParams)
	}
	spendableBankKeeper, _ := options.BankKeeper.(standardMsgSendSpendableBankKeeper)
	ordinaryGasWanted := evmanteevm.NewGasWantedDecorator(
		options.EvmKeeper,
		options.FeeMarketKeeper,
		&feemarketParams,
	)

	return sdk.ChainAnteDecorators(
		cosmosante.NewRejectMessagesDecorator(),
		cosmosante.NewAuthzLimiterDecorator(
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
		),
		NewStandardMsgSendGasDecorator(options.AccountKeeper),
		authante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		authante.NewValidateBasicDecorator(),
		authante.NewTxTimeoutHeightDecorator(),
		authante.NewValidateMemoDecorator(options.AccountKeeper),
		newStandardMsgSendFeePlanDecorator(
			&feemarketParams,
			options.AccountKeeper,
			spendableBankKeeper,
		),
		authante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		newStandardMsgSendDeductFeeDecorator(
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
		newStandardMsgSendGasWantedDecorator(
			ordinaryGasWanted,
			options.FeeMarketKeeper,
			&feemarketParams,
		),
	)
}

// newCosmosDynamicFeeChecker owns the application-wide Cosmos dynamic-fee
// policy. Ordinary transactions preserve the upstream Cosmos EVM v0.6.2 fee,
// priority, fallback, and error ordering, then enforce the global floor against
// the exact dynamic effective price. FixedSendGas transactions use their
// context fee plan instead of this checker.
func newCosmosDynamicFeeChecker(
	feemarketParams *feemarkettypes.Params,
) authante.TxFeeChecker {
	upstream := evmanteevm.NewDynamicFeeChecker(feemarketParams)

	return func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return upstream(ctx, tx)
		}
		fees, priority, err := upstream(ctx, tx)
		if err != nil {
			return fees, priority, err
		}
		if err := checkOrdinaryDynamicFeeFloor(ctx, feeTx, feemarketParams); err != nil {
			return nil, 0, err
		}
		return fees, priority, nil
	}
}

// checkOrdinaryDynamicFeeFloor reconstructs the exact LegacyDec price used by
// the upstream v0.6.2 dynamic checker. It runs only after that checker succeeds,
// preserving its fallback and error ordering without deriving price from the
// rounded effective fee.
func checkOrdinaryDynamicFeeFloor(
	ctx sdk.Context,
	feeTx sdk.FeeTx,
	feemarketParams *feemarkettypes.Params,
) error {
	if ctx.BlockHeight() == 0 ||
		!evmtypes.IsLondon(evmtypes.GetEthChainConfig(), ctx.BlockHeight()) ||
		feemarketParams.MinGasPrice.IsZero() {
		return nil
	}

	dynamicFee, ok := dynamicFeeExtension(feeTx)
	if !ok {
		return nil
	}

	baseFee := feemarketParams.BaseFee
	if baseFee.IsNil() {
		baseFee = sdkmath.LegacyZeroDec()
	}
	tipCap := dynamicFee.MaxPriorityPrice
	if tipCap.IsNil() {
		tipCap = sdkmath.LegacyZeroDec()
	}

	denom := evmtypes.GetEVMCoinDenom()
	gas := sdkmath.NewIntFromUint64(feeTx.GetGas())
	feeAmount := feeTx.GetFee().AmountOf(denom)
	feeCap := sdkmath.LegacyNewDecFromInt(feeAmount).QuoInt(gas)
	effectivePrice := sdkmath.LegacyMinDec(baseFee.Add(tipCap), feeCap)
	if effectivePrice.LT(feemarketParams.MinGasPrice) {
		return errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"effective gas price below minimum global gas price (%s%s < %s%s)",
			effectivePrice,
			denom,
			feemarketParams.MinGasPrice,
			denom,
		)
	}

	return nil
}

func dynamicFeeExtension(feeTx sdk.FeeTx) (*antetypes.ExtensionOptionDynamicFeeTx, bool) {
	extensionTx, ok := feeTx.(authante.HasExtensionOptionsTx)
	if !ok {
		return nil, false
	}
	for _, option := range extensionTx.GetExtensionOptions() {
		if dynamicFee, ok := option.GetCachedValue().(*antetypes.ExtensionOptionDynamicFeeTx); ok {
			return dynamicFee, true
		}
	}
	return nil, false
}
