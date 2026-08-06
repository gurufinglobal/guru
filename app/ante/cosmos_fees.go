package ante

import (
	"bytes"
	"context"
	"fmt"
	"math"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	cosmosevmtypes "github.com/cosmos/evm/ante/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ethparams "github.com/ethereum/go-ethereum/params"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

// EffectiveFeeBreakdown is the admission-time effective fee split into the
// component eligible for a policy discount and the priority tip that must
// remain undiscounted.
type EffectiveFeeBreakdown struct {
	EffectiveFee  sdk.Coins
	BaseComponent sdk.Coins
	TipComponent  sdk.Coins
	Priority      int64
}

// TxFeeChecker computes the effective fee and its exact base/tip split. Tip is
// computed from fee-cap inputs, never inferred from the truncated priority.
type TxFeeChecker func(ctx sdk.Context, tx sdk.Tx) (EffectiveFeeBreakdown, error)

// DeductFeeDecorator applies a policy only to the Cosmos effective base fee,
// consumes any fee grant with the resulting actual fee, and collects that fee.
type DeductFeeDecorator struct {
	accountKeeper      authante.AccountKeeper
	bankKeeper         authtypes.BankKeeper
	feegrantKeeper     authante.FeegrantKeeper
	txFeeChecker       TxFeeChecker
	feePolicyKeeper    FeePolicyKeeper
	feeRecipientModule string
}

// NewDeductFeeDecorator is the application-local extension point for Cosmos
// fee accounting. Collection currently remains a normal bank transfer to the
// SDK fee_collector module account.
func NewDeductFeeDecorator(
	accountKeeper authante.AccountKeeper,
	bankKeeper authtypes.BankKeeper,
	feegrantKeeper authante.FeegrantKeeper,
	txFeeChecker TxFeeChecker,
	feePolicyKeeper FeePolicyKeeper,
) DeductFeeDecorator {
	if txFeeChecker == nil {
		txFeeChecker = checkTxFeeWithValidatorMinGasPrices
	}

	return DeductFeeDecorator{
		accountKeeper:      accountKeeper,
		bankKeeper:         bankKeeper,
		feegrantKeeper:     feegrantKeeper,
		txFeeChecker:       txFeeChecker,
		feePolicyKeeper:    feePolicyKeeper,
		feeRecipientModule: authtypes.FeeCollectorName,
	}
}

func (dfd DeductFeeDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	if err := dfd.validate(); err != nil {
		return ctx, err
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	if !simulate && ctx.BlockHeight() > 0 && feeTx.GetGas() == 0 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidGasLimit, "must provide positive gas")
	}

	breakdown := EffectiveFeeBreakdown{
		EffectiveFee:  feeTx.GetFee(),
		BaseComponent: feeTx.GetFee(),
		TipComponent:  sdk.Coins{},
	}
	if !simulate {
		var err error
		breakdown, err = dfd.txFeeChecker(ctx, tx)
		if err != nil {
			return ctx, err
		}
	}
	if err := validateEffectiveFeeBreakdown(breakdown); err != nil {
		return ctx, err
	}

	if dfd.accountKeeper.GetModuleAddress(dfd.feeRecipientModule) == nil {
		return ctx, fmt.Errorf("fee recipient module account (%s) has not been set", dfd.feeRecipientModule)
	}

	funding, err := dfd.resolveFeeFunding(feeTx)
	if err != nil {
		return ctx, err
	}

	discount, err := dfd.feePolicyKeeper.ResolveDiscount(ctx, funding.canonicalAddress, tx.GetMsgs())
	if err != nil {
		return ctx, err
	}

	discountedBase, err := applyDiscountToBaseFee(breakdown.BaseComponent, discount)
	if err != nil {
		return ctx, err
	}
	actualFee := discountedBase.Add(breakdown.TipComponent...)

	if err := dfd.checkDeductFee(ctx, tx, funding, actualFee); err != nil {
		return ctx, err
	}

	return next(ctx.WithPriority(breakdown.Priority), tx, simulate)
}

func (dfd DeductFeeDecorator) validate() error {
	if dfd.accountKeeper == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "account keeper is required for fee deduction")
	}
	if dfd.bankKeeper == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "bank keeper is required for fee deduction")
	}
	if dfd.txFeeChecker == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "tx fee checker is required for fee deduction")
	}
	if dfd.feePolicyKeeper == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "fee policy keeper is required for fee deduction")
	}

	return nil
}

// validateEffectiveFeeBreakdown enforces the TxFeeChecker contract before any
// policy lookup, feegrant consumption, or bank transfer can occur.
func validateEffectiveFeeBreakdown(breakdown EffectiveFeeBreakdown) error {
	switch {
	case len(breakdown.EffectiveFee) != 0 && !breakdown.EffectiveFee.IsValid():
		return errorsmod.Wrap(sdkerrors.ErrLogic, "tx fee checker returned an invalid effective fee")
	case len(breakdown.BaseComponent) != 0 && !breakdown.BaseComponent.IsValid():
		return errorsmod.Wrap(sdkerrors.ErrLogic, "tx fee checker returned an invalid base component")
	case len(breakdown.TipComponent) != 0 && !breakdown.TipComponent.IsValid():
		return errorsmod.Wrap(sdkerrors.ErrLogic, "tx fee checker returned an invalid tip component")
	}

	recomposedFee := breakdown.BaseComponent.Add(breakdown.TipComponent...)
	if !breakdown.EffectiveFee.Equal(recomposedFee) {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "tx fee checker returned an inconsistent effective fee breakdown")
	}

	return nil
}

type feeFunding struct {
	payer            sdk.AccAddress
	account          sdk.AccAddress
	canonicalAddress string
	hasGranter       bool
	useGrantedFees   bool
}

func (dfd DeductFeeDecorator) resolveFeeFunding(feeTx sdk.FeeTx) (feeFunding, error) {
	payer := feeTx.FeePayer()
	account := payer
	granter := feeTx.FeeGranter()
	hasGranter := granter != nil
	if hasGranter {
		account = granter
	}

	canonicalAddress, err := dfd.accountKeeper.AddressCodec().BytesToString(account)
	if err != nil {
		return feeFunding{}, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, err.Error())
	}

	return feeFunding{
		payer:            payer,
		account:          account,
		canonicalAddress: canonicalAddress,
		hasGranter:       hasGranter,
		useGrantedFees:   hasGranter && !bytes.Equal(account, payer),
	}, nil
}

func (dfd DeductFeeDecorator) checkDeductFee(
	ctx sdk.Context,
	tx sdk.Tx,
	funding feeFunding,
	fee sdk.Coins,
) error {
	if !fee.IsValid() {
		return errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "invalid fee amount: %s", fee)
	}

	if funding.hasGranter {
		if dfd.feegrantKeeper == nil {
			return sdkerrors.ErrInvalidRequest.Wrap("fee grants are not enabled")
		}
		if funding.useGrantedFees {
			if err := dfd.feegrantKeeper.UseGrantedFees(
				ctx,
				funding.account,
				funding.payer,
				fee,
				tx.GetMsgs(),
			); err != nil {
				return errorsmod.Wrapf(err, "%s does not allow to pay fees for %s", funding.account, funding.payer)
			}
		}
	}

	if dfd.accountKeeper.GetAccount(ctx, funding.account) == nil {
		return sdkerrors.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", funding.account)
	}

	if err := dfd.collectFees(ctx, funding.account, fee); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeTx,
			sdk.NewAttribute(sdk.AttributeKeyFee, fee.String()),
			sdk.NewAttribute(sdk.AttributeKeyFeePayer, funding.canonicalAddress),
		),
	)

	return nil
}

// collectFees is the single collection side-effect boundary for Cosmos fees.
// Replacing the collection mechanism in a future task is intentionally local
// to this helper and NewDeductFeeDecorator.
func (dfd DeductFeeDecorator) collectFees(ctx context.Context, payer sdk.AccAddress, fee sdk.Coins) error {
	if fee.IsZero() {
		return nil
	}

	if err := dfd.bankKeeper.SendCoinsFromAccountToModule(
		ctx,
		payer,
		dfd.feeRecipientModule,
		fee,
	); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "%s", err.Error())
	}

	return nil
}

func applyDiscountToBaseFee(baseFee sdk.Coins, discount feepolicytypes.Discount) (sdk.Coins, error) {
	switch discount.DiscountType {
	case "":
		return baseFee, nil
	case feepolicytypes.FeeDiscountTypePercent:
		if discount.Amount.IsNil() || !discount.Amount.IsPositive() || discount.Amount.GT(sdkmath.LegacyNewDec(100)) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid percent fee discount")
		}

		multiplier := sdkmath.LegacyNewDec(100).Sub(discount.Amount).Quo(sdkmath.LegacyNewDec(100))
		return mapDiscountedBaseFee(baseFee, func(original sdkmath.Int) sdkmath.Int {
			return clampDiscountedAmount(
				sdkmath.LegacyNewDecFromInt(original).Mul(multiplier).TruncateInt(),
				original,
			)
		}), nil
	case feepolicytypes.FeeDiscountTypeFixed:
		if discount.Amount.IsNil() || !discount.Amount.IsPositive() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid fixed fee discount")
		}

		fixedAmount := discount.Amount.TruncateInt()
		return mapDiscountedBaseFee(baseFee, func(original sdkmath.Int) sdkmath.Int {
			return clampDiscountedAmount(fixedAmount, original)
		}), nil
	default:
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid fee discount type: %s", discount.DiscountType)
	}
}

func mapDiscountedBaseFee(baseFee sdk.Coins, mapAmount func(sdkmath.Int) sdkmath.Int) sdk.Coins {
	discounted := sdk.Coins{}
	for _, coin := range baseFee {
		discounted = discounted.Add(sdk.NewCoin(coin.Denom, mapAmount(coin.Amount)))
	}
	return discounted
}

func clampDiscountedAmount(amount, original sdkmath.Int) sdkmath.Int {
	if amount.IsNegative() {
		return sdkmath.ZeroInt()
	}
	if amount.GT(original) {
		return original
	}
	return amount
}

// NewDynamicFeeChecker mirrors the Cosmos EVM v0.7 dynamic checker and adds an
// exact base/tip split without deriving tip from the truncated priority.
func NewDynamicFeeChecker(feemarketParams *feemarkettypes.Params) TxFeeChecker {
	return func(ctx sdk.Context, tx sdk.Tx) (EffectiveFeeBreakdown, error) {
		if feemarketParams == nil {
			return EffectiveFeeBreakdown{}, errorsmod.Wrap(sdkerrors.ErrLogic, "fee market params are required")
		}

		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return EffectiveFeeBreakdown{}, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
		}

		if ctx.BlockHeight() == 0 {
			return feeWithValidatorMinGasPrices(ctx, feeTx, dynamicFeePriority)
		}

		return FeeChecker(
			ctx,
			feemarketParams,
			evmtypes.GetEVMCoinDenom(),
			evmtypes.GetEthChainConfig(),
			feeTx,
		)
	}
}

// FeeChecker applies Cosmos EVM v0.7 EIP-1559 fee semantics and retains the
// independently rounded tip component needed for policy accounting.
func FeeChecker(
	ctx sdk.Context,
	feemarketParams *feemarkettypes.Params,
	denom string,
	ethConfig *ethparams.ChainConfig,
	feeTx sdk.FeeTx,
) (EffectiveFeeBreakdown, error) {
	if feemarketParams == nil {
		return EffectiveFeeBreakdown{}, errorsmod.Wrap(sdkerrors.ErrLogic, "fee market params are required")
	}

	if !evmtypes.IsLondon(ethConfig, ctx.BlockHeight()) {
		return feeWithValidatorMinGasPrices(ctx, feeTx, dynamicFeePriority)
	}

	baseFee := feemarketParams.BaseFee
	if baseFee.IsNil() || !feemarketParams.IsBaseFeeEnabled(ctx.BlockHeight()) {
		baseFee = sdkmath.LegacyZeroDec()
	}

	maxPriorityPrice := sdkmath.LegacyNewDec(math.MaxInt64)
	if hasExtensionOptionsTx, ok := feeTx.(authante.HasExtensionOptionsTx); ok {
		for _, option := range hasExtensionOptionsTx.GetExtensionOptions() {
			if dynamicFee, ok := option.GetCachedValue().(*cosmosevmtypes.ExtensionOptionDynamicFeeTx); ok {
				maxPriorityPrice = dynamicFee.MaxPriorityPrice
				if maxPriorityPrice.IsNil() {
					maxPriorityPrice = sdkmath.LegacyZeroDec()
				}
				break
			}
		}
	}

	if maxPriorityPrice.IsNegative() {
		return EffectiveFeeBreakdown{}, errorsmod.Wrap(sdkerrors.ErrInsufficientFee, "max priority price cannot be negative")
	}

	effectiveGas := effectiveFeeGas(ctx, feeTx.GetGas())
	gas := sdkmath.NewIntFromUint64(effectiveGas)
	if gas.IsZero() {
		return EffectiveFeeBreakdown{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "gas cannot be zero")
	}

	feeAmount := sdkmath.LegacyNewDecFromInt(feeTx.GetFee().AmountOf(denom))
	feeCap := feeAmount.QuoInt(gas)
	if feeCap.LT(baseFee) {
		return EffectiveFeeBreakdown{}, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"gas prices too low, got: %s%s required: %s%s. Please retry using a higher gas price or a higher fee",
			feeCap,
			denom,
			baseFee,
			denom,
		)
	}

	effectivePrice := sdkmath.LegacyMinDec(maxPriorityPrice.Add(baseFee), feeCap)
	effectiveAmount := effectivePrice.MulInt(gas).Ceil().RoundInt()
	effectiveFee := canonicalCoin(denom, effectiveAmount)

	tipAmount := effectivePrice.Sub(baseFee).MulInt(gas).Ceil().RoundInt()
	if baseFee.IsZero() && tipAmount.Equal(effectiveAmount) {
		tipAmount = sdkmath.ZeroInt()
	}
	tipComponent := canonicalCoin(denom, tipAmount)
	baseComponent, hasNegative := effectiveFee.SafeSub(tipComponent...)
	if hasNegative {
		return EffectiveFeeBreakdown{}, errorsmod.Wrap(sdkerrors.ErrLogic, "priority tip exceeds effective fee")
	}

	priorityInt := effectivePrice.Sub(baseFee).QuoInt(evmtypes.DefaultPriorityReduction).TruncateInt()
	priority := int64(math.MaxInt64)
	if priorityInt.IsInt64() {
		priority = priorityInt.Int64()
	}

	return EffectiveFeeBreakdown{
		EffectiveFee:  effectiveFee,
		BaseComponent: baseComponent,
		TipComponent:  tipComponent,
		Priority:      priority,
	}, nil
}

func canonicalCoin(denom string, amount sdkmath.Int) sdk.Coins {
	if amount.IsZero() {
		return sdk.Coins{}
	}
	return sdk.Coins{{Denom: denom, Amount: amount}}
}

func checkTxFeeWithValidatorMinGasPrices(ctx sdk.Context, tx sdk.Tx) (EffectiveFeeBreakdown, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return EffectiveFeeBreakdown{}, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	return feeWithValidatorMinGasPrices(ctx, feeTx, sdkDefaultFeePriority)
}

func feeWithValidatorMinGasPrices(
	ctx sdk.Context,
	feeTx sdk.FeeTx,
	priorityFn func(sdk.Coins, int64) int64,
) (EffectiveFeeBreakdown, error) {
	feeCoins := feeTx.GetFee()
	gas := int64(effectiveFeeGas(ctx, feeTx.GetGas())) // #nosec G115 -- ValidateBasic bounds the gas limit before fee deduction.

	if ctx.IsCheckTx() && !ctx.MinGasPrices().IsZero() {
		minGasPrices := ctx.MinGasPrices()
		requiredFees := make(sdk.Coins, len(minGasPrices))
		gasLimit := sdkmath.LegacyNewDec(gas)
		for i, gasPrice := range minGasPrices {
			requiredFee := gasPrice.Amount.Mul(gasLimit)
			requiredFees[i] = sdk.NewCoin(gasPrice.Denom, requiredFee.Ceil().RoundInt())
		}

		if !feeCoins.IsAnyGTE(requiredFees) {
			return EffectiveFeeBreakdown{}, errorsmod.Wrapf(
				sdkerrors.ErrInsufficientFee,
				"insufficient fees; got: %s required: %s",
				feeCoins,
				requiredFees,
			)
		}
	}

	return EffectiveFeeBreakdown{
		EffectiveFee:  feeCoins,
		BaseComponent: feeCoins,
		TipComponent:  sdk.Coins{},
		Priority:      priorityFn(feeCoins, gas),
	}, nil
}

func sdkDefaultFeePriority(fees sdk.Coins, gas int64) int64 {
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

func dynamicFeePriority(fees sdk.Coins, gas int64) int64 {
	var priority int64
	for _, fee := range fees {
		gasPrice := fee.Amount.QuoRaw(gas)
		priorityAmount := gasPrice.Quo(evmtypes.DefaultPriorityReduction)
		candidate := int64(math.MaxInt64)
		if priorityAmount.IsInt64() {
			candidate = priorityAmount.Int64()
		}
		if priority == 0 || candidate < priority {
			priority = candidate
		}
	}
	return priority
}

func effectiveFeeGas(
	ctx sdk.Context,
	declaredGas uint64,
) uint64 {
	if _, ok := ctx.GasMeter().(*standardMsgSendGasMeter); ok {
		return StandardMsgSendGas
	}
	return declaredGas
}
