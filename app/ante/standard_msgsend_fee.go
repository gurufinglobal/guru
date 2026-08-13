package ante

import (
	"fmt"
	"math"
	"math/big"
	"slices"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	cosmosante "github.com/cosmos/evm/ante/cosmos"
	evmanteevm "github.com/cosmos/evm/ante/evm"
	antetypes "github.com/cosmos/evm/ante/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
)

// newStandardMsgSendDynamicFeeChecker leaves ordinary transactions on the
// upstream Cosmos EVM v0.6.1 checker. Eligible MsgSend transactions use the
// Ethereum transaction price model against declared gas D, then settle that
// price against execution gas E.
//
// This application's native EVM denom has 18 decimals. Consequently, the EVM
// path's ConvertAmountTo18DecimalsLegacy step is a no-op, while converting the
// LegacyDec base fee to *big.Int still truncates its fractional component.
func newStandardMsgSendDynamicFeeChecker(
	feemarketParams *feemarkettypes.Params,
) authante.TxFeeChecker {
	upstream := evmanteevm.NewDynamicFeeChecker(feemarketParams)

	return func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		if !isStandardMsgSendGasContext(ctx) {
			return upstream(ctx, tx)
		}

		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return nil, 0, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
		}
		settlementGas, ok := standardMsgSendSettlementGas(ctx)
		if !ok {
			return nil, 0, errorsmod.Wrap(sdkerrors.ErrLogic, "standard MsgSend execution gas is unavailable")
		}
		if ctx.BlockHeight() == 0 ||
			!evmtypes.IsLondon(evmtypes.GetEthChainConfig(), ctx.BlockHeight()) {
			fees, priority, err := upstream(ctx, tx)
			if err != nil || isStandardMsgSendAdmissionContext(ctx) {
				return fees, priority, err
			}
			settled, settleErr := settleStandardMsgSendFees(
				fees,
				feeTx.GetGas(),
				settlementGas,
			)
			return settled, priority, settleErr
		}

		if isStandardMsgSendAdmissionContext(ctx) {
			return checkAndSettleStandardMsgSendFee(
				ctx,
				feeTx,
				feemarketParams,
				feeTx.GetGas(),
			)
		}
		return checkAndSettleStandardMsgSendFee(
			ctx,
			feeTx,
			feemarketParams,
			settlementGas,
		)
	}
}

// newStandardMsgSendValidatorFeeChecker mirrors the Cosmos SDK v0.53
// validator-min-gas-price checker. Admission and priority use the declared gas;
// eligible MsgSend settlement alone uses execution gas.
func newStandardMsgSendValidatorFeeChecker() authante.TxFeeChecker {
	return func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		fees, priority, err := checkStandardMsgSendValidatorFee(ctx, tx)
		if err != nil || !isStandardMsgSendGasContext(ctx) {
			return fees, priority, err
		}
		if isStandardMsgSendAdmissionContext(ctx) {
			return fees, priority, nil
		}

		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return nil, 0, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
		}

		settlementGas, ok := standardMsgSendSettlementGas(ctx)
		if !ok {
			return fees, priority, nil
		}

		settled, settleErr := settleStandardMsgSendFees(
			fees,
			feeTx.GetGas(),
			settlementGas,
		)
		return settled, priority, settleErr
	}
}

func isStandardMsgSendAdmissionContext(ctx sdk.Context) bool {
	return ctx.IsCheckTx() || ctx.IsReCheckTx()
}

func isStandardMsgSendGasContext(ctx sdk.Context) bool {
	_, ok := ctx.GasMeter().(*standardMsgSendGasMeter)
	return ok
}

func standardMsgSendSettlementGas(ctx sdk.Context) (uint64, bool) {
	meter, ok := ctx.GasMeter().(*standardMsgSendGasMeter)
	if !ok {
		return 0, false
	}
	return uint64(meter.executionGas), true
}

// checkAndSettleStandardMsgSendFee applies the successful Ethereum plain
// transfer price semantics:
//
//   - legacy/no extension: P = F = submittedFee / D
//   - dynamic extension: P = min(B + tip, F)
//   - admission and priority use D; deduction is ceil(P * E)
//
// B is the integer EVM base fee. NoBaseFee and pre-enable heights produce B=0.
func checkAndSettleStandardMsgSendFee(
	ctx sdk.Context,
	feeTx sdk.FeeTx,
	feemarketParams *feemarkettypes.Params,
	executionGas uint64,
) (sdk.Coins, int64, error) {
	declaredGas := feeTx.GetGas()
	if declaredGas == 0 {
		return nil, 0, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "gas cannot be zero")
	}

	denom := evmtypes.GetEVMCoinDenom()
	declaredGasInt := sdkmath.NewIntFromUint64(declaredGas)
	feeAmount := feeTx.GetFee().AmountOfNoDenomValidation(denom) //nolint:staticcheck // matches Cosmos EVM v0.6.1
	feeCap := sdkmath.LegacyNewDecFromInt(feeAmount).QuoInt(declaredGasInt)
	baseFee := standardMsgSendEthereumBaseFee(ctx, feemarketParams)

	dynamicFee, hasDynamicFee := standardMsgSendDynamicFee(feeTx)
	effectivePrice := feeCap
	priorityPrice := feeCap.Sub(baseFee)
	if hasDynamicFee {
		tipCap := dynamicFee.MaxPriorityPrice
		if tipCap.IsNil() {
			tipCap = sdkmath.LegacyZeroDec()
		}
		if tipCap.IsNegative() {
			return nil, 0, errorsmod.Wrap(sdkerrors.ErrInsufficientFee, "max priority price cannot be negative")
		}
		if tipCap.GT(feeCap) {
			return nil, 0, errorsmod.Wrapf(
				sdkerrors.ErrInsufficientFee,
				"max priority price %s exceeds fee cap %s",
				tipCap,
				feeCap,
			)
		}
		if feeCap.LT(baseFee) {
			return nil, 0, errorsmod.Wrapf(
				sdkerrors.ErrInsufficientFee,
				"gas prices too low, got: %s%s required: %s%s. Please retry using a higher gas price or a higher fee",
				feeCap,
				denom,
				baseFee,
				denom,
			)
		}

		effectivePrice = sdkmath.LegacyMinDec(baseFee.Add(tipCap), feeCap)
		priorityPrice = effectivePrice.Sub(baseFee)
	} else if feeCap.LT(baseFee) {
		// Ethereum legacy transactions must cover the current base fee.
		return nil, 0, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"gas prices too low, got: %s%s required: %s%s. Please retry using a higher gas price or a higher fee",
			feeCap,
			denom,
			baseFee,
			denom,
		)
	}

	priorityInt := priorityPrice.QuoInt(evmtypes.DefaultPriorityReduction).TruncateInt()
	priority := int64(math.MaxInt64)
	if priorityInt.IsInt64() {
		priority = priorityInt.Int64()
	}

	settledAmount := effectivePrice.
		MulInt(sdkmath.NewIntFromUint64(executionGas)).
		Ceil().
		RoundInt()
	return sdk.Coins{{Denom: denom, Amount: settledAmount}}, priority, nil
}

func standardMsgSendEthereumBaseFee(
	ctx sdk.Context,
	feemarketParams *feemarkettypes.Params,
) sdkmath.LegacyDec {
	if feemarketParams == nil || !feemarketParams.IsBaseFeeEnabled(ctx.BlockHeight()) {
		return sdkmath.LegacyZeroDec()
	}

	baseFee := feemarketParams.BaseFee
	if baseFee.IsNil() || baseFee.IsZero() {
		return sdkmath.LegacyZeroDec()
	}

	// The application config fixes the native EVM denom exponent to 18, so
	// ConvertAmountTo18DecimalsLegacy is intentionally not needed here.
	return sdkmath.LegacyNewDecFromInt(baseFee.TruncateInt())
}

func standardMsgSendDynamicFee(feeTx sdk.FeeTx) (*antetypes.ExtensionOptionDynamicFeeTx, bool) {
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

// standardMsgSendMinGasPriceDecorator preserves the upstream Cosmos decorator
// for ordinary transactions. For an eligible MsgSend it checks the Ethereum
// effective price P against the global minimum price, both multiplied by D.
type standardMsgSendMinGasPriceDecorator struct {
	upstream        cosmosante.MinGasPriceDecorator
	feemarketParams *feemarkettypes.Params
}

func newStandardMsgSendMinGasPriceDecorator(
	feemarketParams *feemarkettypes.Params,
) standardMsgSendMinGasPriceDecorator {
	return standardMsgSendMinGasPriceDecorator{
		upstream:        cosmosante.NewMinGasPriceDecorator(feemarketParams),
		feemarketParams: feemarketParams,
	}
}

func (d standardMsgSendMinGasPriceDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	if !isStandardMsgSendGasContext(ctx) {
		return d.upstream.AnteHandle(ctx, tx, simulate, next)
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidType, "invalid transaction type %T, expected sdk.FeeTx", tx)
	}

	feeCoins := feeTx.GetFee()
	denom := evmtypes.GetEVMCoinDenom()
	validFees := len(feeCoins) == 0 ||
		(len(feeCoins) == 1 && slices.Contains([]string{denom, sdk.DefaultBondDenom}, feeCoins.GetDenomByIndex(0)))
	if !validFees && !simulate {
		return ctx, fmt.Errorf("expected only native token %s for fee, but got %s", denom, feeCoins.String())
	}
	if d.feemarketParams.MinGasPrice.IsZero() || simulate {
		return next(ctx, tx, simulate)
	}
	if feeTx.GetGas() == 0 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "gas cannot be zero")
	}

	feeCap := sdkmath.LegacyNewDecFromInt(feeCoins.AmountOfNoDenomValidation(denom)). //nolint:staticcheck // matches v0.6.1 native-denom handling
												QuoInt(sdkmath.NewIntFromUint64(feeTx.GetGas()))
	effectivePrice := feeCap
	if dynamicFee, ok := standardMsgSendDynamicFee(feeTx); ok {
		tipCap := dynamicFee.MaxPriorityPrice
		if tipCap.IsNil() {
			tipCap = sdkmath.LegacyZeroDec()
		}
		if tipCap.IsNegative() {
			return ctx, errorsmod.Wrap(sdkerrors.ErrInsufficientFee, "max priority price cannot be negative")
		}
		if tipCap.GT(feeCap) {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "max priority price %s exceeds fee cap %s", tipCap, feeCap)
		}
		baseFee := standardMsgSendEthereumBaseFee(ctx, d.feemarketParams)
		if feeCap.LT(baseFee) {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "gas prices too low, got: %s%s required: %s%s", feeCap, denom, baseFee, denom)
		}
		effectivePrice = sdkmath.LegacyMinDec(baseFee.Add(tipCap), feeCap)
	}

	gasLimit := sdkmath.LegacyNewDecFromBigInt(new(big.Int).SetUint64(feeTx.GetGas()))
	providedFee := effectivePrice.Mul(gasLimit)
	requiredFee := d.feemarketParams.MinGasPrice.Mul(gasLimit)
	if providedFee.LT(requiredFee) {
		return ctx, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"provided fee < minimum global fee (%s < %s). Please increase the priority tip (for EIP-1559 txs) or the gas prices (for legacy txs)",
			providedFee.TruncateInt(),
			requiredFee.TruncateInt(),
		)
	}

	return next(ctx, tx, simulate)
}

// settleStandardMsgSendFees preserves each submitted coin's gas price while
// replacing the declared gas multiplier with executionGas.
func settleStandardMsgSendFees(
	fees sdk.Coins,
	declaredGas uint64,
	executionGas uint64,
) (sdk.Coins, error) {
	if declaredGas == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "gas cannot be zero")
	}

	declaredGasInt := sdkmath.NewIntFromUint64(declaredGas)
	settlementGas := sdkmath.NewIntFromUint64(executionGas)
	settled := make(sdk.Coins, len(fees))
	for i, fee := range fees {
		amount := sdkmath.LegacyNewDecFromInt(fee.Amount).
			QuoInt(declaredGasInt).
			MulInt(settlementGas).
			Ceil().
			RoundInt()
		settled[i] = sdk.NewCoin(fee.Denom, amount)
	}

	return settled, nil
}

// checkStandardMsgSendValidatorFee is the local equivalent of the unexported
// Cosmos SDK v0.53 default checker. It intentionally evaluates the original
// FeeTx and its declared gas before execution-gas settlement occurs.
func checkStandardMsgSendValidatorFee(
	ctx sdk.Context,
	tx sdk.Tx,
) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	fees := feeTx.GetFee()
	gas := feeTx.GetGas()
	if ctx.IsCheckTx() && !ctx.MinGasPrices().IsZero() {
		requiredFees := make(sdk.Coins, len(ctx.MinGasPrices()))
		gasLimit := sdkmath.LegacyNewDec(int64(gas)) // #nosec G115 -- ValidateBasic bounds gas before fee deduction.
		for i, gasPrice := range ctx.MinGasPrices() {
			requiredFee := gasPrice.Amount.Mul(gasLimit)
			requiredFees[i] = sdk.NewCoin(
				gasPrice.Denom,
				requiredFee.Ceil().RoundInt(),
			)
		}

		if !fees.IsAnyGTE(requiredFees) {
			return nil, 0, errorsmod.Wrapf(
				sdkerrors.ErrInsufficientFee,
				"insufficient fees; got: %s required: %s",
				fees,
				requiredFees,
			)
		}
	}

	return fees, standardMsgSendValidatorPriority(fees, int64(gas)), nil // #nosec G115 -- ValidateBasic bounds gas.
}

func standardMsgSendValidatorPriority(fees sdk.Coins, gas int64) int64 {
	if gas <= 0 {
		return 0
	}

	var priority int64
	for _, fee := range fees {
		candidate := int64(math.MaxInt64)
		gasPrice := fee.Amount.QuoRaw(gas)
		if gasPrice.IsInt64() {
			candidate = gasPrice.Int64()
		}
		if priority == 0 || candidate < priority {
			priority = candidate
		}
	}
	return priority
}
