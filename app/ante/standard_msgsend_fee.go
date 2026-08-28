package ante

import (
	"context"
	"fmt"
	"math"
	"math/big"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	cosmosante "github.com/cosmos/evm/ante/cosmos"
	evmanteevm "github.com/cosmos/evm/ante/evm"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

const (
	EventTypeFixedSendGas           = "fixed_send_gas"
	AttributeKeyDeclaredGas         = "declared_gas"
	AttributeKeyAccountingGas       = "accounting_gas"
	AttributeKeySubmittedFee        = "submitted_fee"
	AttributeKeyEffectiveGasPrice   = "effective_gas_price"
	AttributeKeyActualFee           = "actual_fee"
	standardMsgSendDecimalPrecision = 18
)

type standardMsgSendFeePlanContextKey struct{}

type standardMsgSendSpendableBankKeeper interface {
	SpendableCoin(context.Context, sdk.AccAddress, string) sdk.Coin
}

type standardMsgSendPrice struct {
	numerator   *big.Int
	denominator *big.Int
}

type standardMsgSendFeePlan struct {
	declaredGas    uint64
	submittedFee   sdk.Coin
	effectivePrice standardMsgSendPrice
	actualFee      sdk.Coins
	priority       int64
}

func standardMsgSendDecimalScale() *big.Int {
	return new(big.Int).Exp(
		big.NewInt(10),
		big.NewInt(standardMsgSendDecimalPrecision),
		nil,
	)
}

func standardMsgSendScaledDec(value sdkmath.LegacyDec) *big.Int {
	if value.IsNil() {
		return new(big.Int)
	}
	return value.BigInt()
}

func compareStandardMsgSendPrices(left, right standardMsgSendPrice) int {
	leftProduct := new(big.Int).Mul(left.numerator, right.denominator)
	rightProduct := new(big.Int).Mul(right.numerator, left.denominator)
	return leftProduct.Cmp(rightProduct)
}

func ceilStandardMsgSendQuotient(numerator, denominator *big.Int) *big.Int {
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func standardMsgSendPriceString(price standardMsgSendPrice) string {
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(price.numerator, price.denominator, remainder)
	if remainder.Sign() == 0 {
		return quotient.String()
	}
	return fmt.Sprintf("%s/%s", price.numerator, price.denominator)
}

// buildStandardMsgSendFeePlan is the only FixedSendGas fee formula. It uses
// exact integer cross-products throughout: feeCap F/D is never rounded into a
// LegacyDec before BaseFee, MGP, settlement, or priority is decided.
func buildStandardMsgSendFeePlan(
	ctx sdk.Context,
	classification *standardMsgSendClassification,
	feemarketParams *feemarkettypes.Params,
	accountKeeper authante.AccountKeeper,
	bankKeeper standardMsgSendSpendableBankKeeper,
	checkSpendability bool,
) (standardMsgSendFeePlan, error) {
	if classification == nil || classification.feeTx == nil || classification.msg == nil {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas classification is unavailable",
		)
	}
	if feemarketParams == nil {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas fee market params are not configured",
		)
	}
	if feemarketParams.MinGasPrice.IsNil() || feemarketParams.MinGasPrice.IsNegative() {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas minimum gas price must be configured and non-negative",
		)
	}

	feeTx := classification.feeTx
	declaredGas := feeTx.GetGas()
	if declaredGas < StandardMsgSendGas {
		return standardMsgSendFeePlan{}, errorsmod.Wrapf(
			sdkerrors.ErrInvalidGasLimit,
			"FixedSendGas requires at least %d declared gas",
			StandardMsgSendGas,
		)
	}
	fees := feeTx.GetFee()
	if len(fees) != 1 || fees[0].Denom != chainconfig.BaseDenom || !fees[0].IsPositive() {
		return standardMsgSendFeePlan{}, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"FixedSendGas requires one positive %s fee",
			chainconfig.BaseDenom,
		)
	}

	declaredGasInt := new(big.Int).SetUint64(declaredGas)
	submittedFeeInt := fees[0].Amount.BigInt()
	decimalScale := standardMsgSendDecimalScale()
	feeCap := standardMsgSendPrice{
		numerator:   new(big.Int).Set(submittedFeeInt),
		denominator: declaredGasInt,
	}

	if standardMsgSendBaseFeeEnabled(ctx, feemarketParams) && feemarketParams.BaseFee.IsNil() {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas active base fee is not configured",
		)
	}
	baseFee := standardMsgSendBaseFee(ctx, feemarketParams)
	if baseFee.IsNegative() {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas base fee cannot be negative",
		)
	}
	baseFeeScaled := standardMsgSendScaledDec(baseFee)
	baseFeePrice := standardMsgSendPrice{
		numerator:   baseFeeScaled,
		denominator: decimalScale,
	}
	if compareStandardMsgSendPrices(feeCap, baseFeePrice) < 0 {
		return standardMsgSendFeePlan{}, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"FixedSendGas fee cap below base fee (%s%s < %s%s)",
			standardMsgSendPriceString(feeCap),
			chainconfig.BaseDenom,
			baseFee,
			chainconfig.BaseDenom,
		)
	}

	effectivePrice := feeCap
	if dynamicFee, ok := dynamicFeeExtension(feeTx); ok {
		tipCap := dynamicFee.MaxPriorityPrice
		if tipCap.IsNil() {
			tipCap = sdkmath.LegacyZeroDec()
		}
		if tipCap.IsNegative() {
			return standardMsgSendFeePlan{}, errorsmod.Wrap(
				sdkerrors.ErrInsufficientFee,
				"max priority price cannot be negative",
			)
		}
		basePlusTip := standardMsgSendPrice{
			numerator: new(big.Int).Add(
				baseFeeScaled,
				standardMsgSendScaledDec(tipCap),
			),
			denominator: decimalScale,
		}
		if compareStandardMsgSendPrices(basePlusTip, feeCap) < 0 {
			effectivePrice = basePlusTip
		}
	}

	minimumPrice := feemarketParams.MinGasPrice
	minimumPriceRatio := standardMsgSendPrice{
		numerator:   standardMsgSendScaledDec(minimumPrice),
		denominator: decimalScale,
	}
	if compareStandardMsgSendPrices(effectivePrice, minimumPriceRatio) < 0 {
		return standardMsgSendFeePlan{}, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"FixedSendGas effective gas price below minimum global gas price (%s%s < %s%s)",
			standardMsgSendPriceString(effectivePrice),
			chainconfig.BaseDenom,
			minimumPrice,
			chainconfig.BaseDenom,
		)
	}

	actualFeeInt := ceilStandardMsgSendQuotient(
		new(big.Int).Mul(
			new(big.Int).Set(effectivePrice.numerator),
			new(big.Int).SetUint64(StandardMsgSendGas),
		),
		effectivePrice.denominator,
	)
	if actualFeeInt.Cmp(submittedFeeInt) > 0 {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas actual fee exceeds submitted fee",
		)
	}

	actualFee := sdk.NewCoins()
	if actualFeeInt.Sign() > 0 {
		actualFee = sdk.NewCoins(sdk.NewCoin(
			chainconfig.BaseDenom,
			sdkmath.NewIntFromBigInt(actualFeeInt),
		))
	}

	priorityNumerator := new(big.Int).Sub(
		new(big.Int).Mul(
			new(big.Int).Set(effectivePrice.numerator),
			decimalScale,
		),
		new(big.Int).Mul(
			baseFeeScaled,
			effectivePrice.denominator,
		),
	)
	if priorityNumerator.Sign() < 0 {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas effective price is below base fee",
		)
	}
	priorityReduction := evmtypes.DefaultPriorityReduction.BigInt()
	if priorityReduction == nil || priorityReduction.Sign() <= 0 {
		return standardMsgSendFeePlan{}, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas priority reduction must be positive",
		)
	}
	priorityDenominator := new(big.Int).Mul(
		new(big.Int).Mul(
			new(big.Int).Set(effectivePrice.denominator),
			decimalScale,
		),
		priorityReduction,
	)
	priorityInt := new(big.Int).Quo(priorityNumerator, priorityDenominator)
	priority := int64(math.MaxInt64)
	if priorityInt.IsInt64() {
		priority = priorityInt.Int64()
	}

	if checkSpendability {
		if accountKeeper == nil {
			return standardMsgSendFeePlan{}, errorsmod.Wrap(
				sdkerrors.ErrLogic,
				"FixedSendGas account keeper is not configured",
			)
		}
		if accountKeeper.GetAccount(ctx, classification.sender) == nil {
			return standardMsgSendFeePlan{}, errorsmod.Wrapf(
				sdkerrors.ErrUnknownAddress,
				"fee payer address %s does not exist",
				classification.sender,
			)
		}
		if bankKeeper == nil {
			return standardMsgSendFeePlan{}, errorsmod.Wrap(
				sdkerrors.ErrLogic,
				"FixedSendGas spendable bank keeper is not configured",
			)
		}
		spendable := bankKeeper.SpendableCoin(
			ctx,
			classification.sender,
			chainconfig.BaseDenom,
		).Amount.BigInt()
		required := new(big.Int).Add(
			classification.msg.Amount[0].Amount.BigInt(),
			submittedFeeInt,
		)
		if spendable.Cmp(required) < 0 {
			return standardMsgSendFeePlan{}, errorsmod.Wrapf(
				sdkerrors.ErrInsufficientFunds,
				"FixedSendGas spendable balance %s%s is below transfer plus submitted fee %s%s",
				spendable,
				chainconfig.BaseDenom,
				required,
				chainconfig.BaseDenom,
			)
		}
	}

	return standardMsgSendFeePlan{
		declaredGas:    declaredGas,
		submittedFee:   fees[0],
		effectivePrice: effectivePrice,
		actualFee:      actualFee,
		priority:       priority,
	}, nil
}

func standardMsgSendBaseFee(
	ctx sdk.Context,
	feemarketParams *feemarkettypes.Params,
) sdkmath.LegacyDec {
	if !standardMsgSendBaseFeeEnabled(ctx, feemarketParams) ||
		feemarketParams.BaseFee.IsNil() {
		return sdkmath.LegacyZeroDec()
	}
	return feemarketParams.BaseFee
}

func standardMsgSendBaseFeeEnabled(
	ctx sdk.Context,
	feemarketParams *feemarkettypes.Params,
) bool {
	return ctx.BlockHeight() != 0 &&
		evmtypes.IsLondon(evmtypes.GetEthChainConfig(), ctx.BlockHeight()) &&
		feemarketParams != nil &&
		feemarketParams.IsBaseFeeEnabled(ctx.BlockHeight())
}

type standardMsgSendFeePlanDecorator struct {
	upstream        cosmosante.MinGasPriceDecorator
	feemarketParams *feemarkettypes.Params
	accountKeeper   authante.AccountKeeper
	bankKeeper      standardMsgSendSpendableBankKeeper
}

func newStandardMsgSendFeePlanDecorator(
	feemarketParams *feemarkettypes.Params,
	accountKeeper authante.AccountKeeper,
	bankKeeper standardMsgSendSpendableBankKeeper,
) standardMsgSendFeePlanDecorator {
	return standardMsgSendFeePlanDecorator{
		upstream:        cosmosante.NewMinGasPriceDecorator(feemarketParams),
		feemarketParams: feemarketParams,
		accountKeeper:   accountKeeper,
		bankKeeper:      bankKeeper,
	}
}

func (d standardMsgSendFeePlanDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	classification, fixed := standardMsgSendClassificationFromContext(ctx)
	if !fixed {
		return d.upstream.AnteHandle(ctx, tx, simulate, next)
	}
	if simulate && classification.feeTx.GetGas() == 0 {
		return next(ctx.WithPriority(0), tx, simulate)
	}

	plan, err := buildStandardMsgSendFeePlan(
		ctx,
		classification,
		d.feemarketParams,
		d.accountKeeper,
		d.bankKeeper,
		true,
	)
	if err != nil {
		return ctx, err
	}
	return next(
		ctx.WithValue(standardMsgSendFeePlanContextKey{}, &plan),
		tx,
		simulate,
	)
}

func standardMsgSendFeePlanFromContext(ctx sdk.Context) (*standardMsgSendFeePlan, bool) {
	plan, ok := ctx.Value(standardMsgSendFeePlanContextKey{}).(*standardMsgSendFeePlan)
	return plan, ok && plan != nil
}

type standardMsgSendDeductFeeDecorator struct {
	ordinary authante.DeductFeeDecorator
	fixed    authante.DeductFeeDecorator
}

func newStandardMsgSendDeductFeeDecorator(
	accountKeeper authante.AccountKeeper,
	bankKeeper authtypes.BankKeeper,
	feegrantKeeper authante.FeegrantKeeper,
	ordinaryChecker authante.TxFeeChecker,
) standardMsgSendDeductFeeDecorator {
	fixedChecker := func(ctx sdk.Context, _ sdk.Tx) (sdk.Coins, int64, error) {
		plan, ok := standardMsgSendFeePlanFromContext(ctx)
		if !ok {
			return nil, 0, errorsmod.Wrap(
				sdkerrors.ErrLogic,
				"FixedSendGas fee plan is unavailable",
			)
		}
		return plan.actualFee, plan.priority, nil
	}

	return standardMsgSendDeductFeeDecorator{
		ordinary: authante.NewDeductFeeDecorator(
			accountKeeper,
			bankKeeper,
			feegrantKeeper,
			ordinaryChecker,
		),
		fixed: authante.NewDeductFeeDecorator(
			accountKeeper,
			bankKeeper,
			feegrantKeeper,
			fixedChecker,
		),
	}
}

func (d standardMsgSendDeductFeeDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	classification, fixed := standardMsgSendClassificationFromContext(ctx)
	if !fixed {
		return d.ordinary.AnteHandle(ctx, tx, simulate, next)
	}
	if simulate && classification.feeTx.GetGas() == 0 {
		return next(ctx.WithPriority(0), tx, simulate)
	}

	plan, ok := standardMsgSendFeePlanFromContext(ctx)
	if !ok {
		return ctx, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas fee plan is unavailable",
		)
	}
	return d.fixed.AnteHandle(
		ctx,
		tx,
		false,
		func(nextCtx sdk.Context, nextTx sdk.Tx, _ bool) (sdk.Context, error) {
			nextCtx.EventManager().EmitEvent(sdk.NewEvent(
				EventTypeFixedSendGas,
				sdk.NewAttribute(AttributeKeyDeclaredGas, fmt.Sprintf("%d", plan.declaredGas)),
				sdk.NewAttribute(AttributeKeyAccountingGas, fmt.Sprintf("%d", StandardMsgSendGas)),
				sdk.NewAttribute(AttributeKeySubmittedFee, plan.submittedFee.String()),
				sdk.NewAttribute(AttributeKeyEffectiveGasPrice, standardMsgSendPriceString(plan.effectivePrice)),
				sdk.NewAttribute(AttributeKeyActualFee, plan.actualFee.String()),
			))
			return next(nextCtx, nextTx, simulate)
		},
	)
}

type standardMsgSendGasWantedDecorator struct {
	ordinary        evmanteevm.GasWantedDecorator
	feeMarketKeeper interface {
		AddTransientGasWanted(sdk.Context, uint64) (uint64, error)
	}
	feemarketParams *feemarkettypes.Params
}

func newStandardMsgSendGasWantedDecorator(
	ordinary evmanteevm.GasWantedDecorator,
	feeMarketKeeper interface {
		AddTransientGasWanted(sdk.Context, uint64) (uint64, error)
	},
	feemarketParams *feemarkettypes.Params,
) standardMsgSendGasWantedDecorator {
	return standardMsgSendGasWantedDecorator{
		ordinary:        ordinary,
		feeMarketKeeper: feeMarketKeeper,
		feemarketParams: feemarketParams,
	}
}

func (d standardMsgSendGasWantedDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	if !isStandardMsgSendGasContext(ctx) {
		return d.ordinary.AnteHandle(ctx, tx, simulate, next)
	}

	newCtx, err := next(ctx, tx, simulate)
	if err != nil {
		return newCtx, err
	}
	if newCtx.ExecMode() != sdk.ExecModeFinalize ||
		!evmtypes.IsLondon(evmtypes.GetEthChainConfig(), newCtx.BlockHeight()) {
		return newCtx, nil
	}
	if d.feemarketParams == nil {
		return newCtx, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas fee market params are not configured",
		)
	}
	if !d.feemarketParams.IsBaseFeeEnabled(newCtx.BlockHeight()) {
		return newCtx, nil
	}
	if d.feeMarketKeeper == nil {
		return newCtx, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas fee market keeper is not configured",
		)
	}
	if _, err := d.feeMarketKeeper.AddTransientGasWanted(newCtx, StandardMsgSendGas); err != nil {
		return newCtx, errorsmod.Wrap(err, "add FixedSendGas transient gas wanted")
	}
	return newCtx, nil
}
