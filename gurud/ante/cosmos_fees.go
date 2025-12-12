package ante

import (
	"bytes"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/types"

	evmante "github.com/gurufinglobal/guru/v2/ante/evm"
	feepolicytypes "github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

// DeductFeeDecorator deducts fees from the fee payer. The fee payer is the fee granter (if specified) or first signer of the tx.
// If the fee payer does not have the funds to pay for the fees, return an InsufficientFunds error.
// Call next AnteHandler if fees successfully deducted.
// CONTRACT: Tx must implement FeeTx interface to use DeductFeeDecorator
type DeductFeeDecorator struct {
	accountKeeper   authante.AccountKeeper
	bankKeeper      types.BankKeeper
	feegrantKeeper  authante.FeegrantKeeper
	txFeeChecker    evmante.TxFeeChecker
	feepolicyKeeper FeePolicyKeeper
}

func NewDeductFeeDecorator(ak authante.AccountKeeper, bk types.BankKeeper, fk authante.FeegrantKeeper, tfc evmante.TxFeeChecker, fpk FeePolicyKeeper) DeductFeeDecorator {
	if tfc == nil {
		tfc = checkTxFeeWithValidatorMinGasPrices
	}

	return DeductFeeDecorator{
		accountKeeper:   ak,
		bankKeeper:      bk,
		feegrantKeeper:  fk,
		txFeeChecker:    tfc,
		feepolicyKeeper: fpk,
	}
}

func (dfd DeductFeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	if !simulate && ctx.BlockHeight() > 0 && feeTx.GetGas() == 0 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidGasLimit, "must provide positive gas")
	}

	var (
		priority int64
		err      error
	)

	fee := feeTx.GetFee()
	tips := sdk.Coins{}
	if !simulate {
		fee, tips, priority, err = dfd.txFeeChecker(ctx, tx)
		if err != nil {
			return ctx, err
		}
	}

	addrCodec := address.Bech32Codec{
		Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix(),
	}
	feePayer, err := addrCodec.BytesToString(feeTx.FeePayer())
	if err != nil {
		return ctx, err
	}
	feeGranter := feePayer
	if feeTx.FeeGranter() != nil {
		feeGranter, err = addrCodec.BytesToString(feeTx.FeeGranter())
		if err != nil {
			return ctx, err
		}
	}

	discount := dfd.feepolicyKeeper.GetDiscount(ctx, feeGranter, tx.GetMsgs())

	// apply discounts
	var deductedFee sdk.Coins
	baseFee := fee.Sub(tips...)

	switch discount.DiscountType {
	case feepolicytypes.FeeDiscountTypePercent:
		for _, f := range baseFee {
			// Calculate percentage multiplier as (100 - discountAmount) / 100
			// Example: if discount = 25.5%, multiplier = 0.745
			discountMultiplier := math.LegacyNewDec(100).Sub(discount.Amount).Quo(math.LegacyNewDec(100))

			// Apply multiplier to base fee amount
			finalAmt := math.LegacyNewDecFromInt(f.Amount).Mul(discountMultiplier).TruncateInt()

			deductedFee = deductedFee.Add(sdk.NewCoin(f.Denom, finalAmt))
		}
	case feepolicytypes.FeeDiscountTypeFixed:
		for _, f := range baseFee {
			// type: "fixed"
			deductedFee = deductedFee.Add(sdk.NewCoin(f.Denom, discount.Amount.TruncateInt()))
		}
	default:
		// if no discount, deduct full fee
		deductedFee = baseFee
	}
	deductedFee = deductedFee.Add(tips...)

	if err = dfd.checkDeductFee(ctx, tx, deductedFee); err != nil {
		return ctx, err
	}

	newCtx := ctx.WithPriority(priority)

	return next(newCtx, tx, simulate)
}

func (dfd DeductFeeDecorator) checkDeductFee(ctx sdk.Context, sdkTx sdk.Tx, fee sdk.Coins) error {
	feeTx, ok := sdkTx.(sdk.FeeTx)
	if !ok {
		return errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	if addr := dfd.accountKeeper.GetModuleAddress(types.FeeCollectorName); addr == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", types.FeeCollectorName)
	}

	feePayer := feeTx.FeePayer()
	feeGranter := feeTx.FeeGranter()
	deductFeesFrom := feePayer

	// if feegranter set deduct fee from feegranter account.
	// this works with only when feegrant enabled.
	if feeGranter != nil {
		feeGranterAddr := sdk.AccAddress(feeGranter)

		if dfd.feegrantKeeper == nil {
			return sdkerrors.ErrInvalidRequest.Wrap("fee grants are not enabled")
		} else if !bytes.Equal(feeGranterAddr, feePayer) {
			err := dfd.feegrantKeeper.UseGrantedFees(ctx, feeGranterAddr, feePayer, fee, sdkTx.GetMsgs())
			if err != nil {
				return errorsmod.Wrapf(err, "%s does not allow to pay fees for %s", feeGranter, feePayer)
			}
		}

		deductFeesFrom = feeGranterAddr
	}

	deductFeesFromAcc := dfd.accountKeeper.GetAccount(ctx, deductFeesFrom)
	if deductFeesFromAcc == nil {
		return sdkerrors.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", deductFeesFrom)
	}

	// deduct the fees
	if !fee.IsZero() {
		err := authante.DeductFees(dfd.bankKeeper, ctx, deductFeesFromAcc, fee)
		if err != nil {
			return err
		}
	}

	events := sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeTx,
			sdk.NewAttribute(sdk.AttributeKeyFee, fee.String()),
			sdk.NewAttribute(sdk.AttributeKeyFeePayer, sdk.AccAddress(deductFeesFrom).String()),
		),
	}
	ctx.EventManager().EmitEvents(events)

	return nil
}
