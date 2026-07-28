package types

import (
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	FeeDiscountTypePercent = "percent"
	FeeDiscountTypeFixed   = "fixed"
)

// ValidateFeeDiscount validates one message fee rule. MsgType is matched
// later against the exact sdk.MsgTypeURL; it is deliberately not normalized.
func ValidateFeeDiscount(discount Discount) error {
	if discount.DiscountType != FeeDiscountTypePercent && discount.DiscountType != FeeDiscountTypeFixed {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid discount type %q; accepted types are %q and %q", discount.DiscountType, FeeDiscountTypePercent, FeeDiscountTypeFixed)
	}
	if discount.MsgType == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "msg type is required")
	}
	if discount.Amount.IsNil() || !discount.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "discount value must be greater than 0")
	}
	if discount.DiscountType == FeeDiscountTypePercent && discount.Amount.GT(sdkmath.LegacyNewDec(100)) {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "percent discount must be less than or equal to 100")
	}

	return nil
}

// ValidateAccountDiscount validates policy structure. Address decoding and
// canonicalization require the app-injected EVM address codec and are owned by
// the keeper. An empty address denotes the global policy.
func ValidateAccountDiscount(discount AccountDiscount) error {
	modules := make(map[string]struct{}, len(discount.Modules))
	for _, moduleDiscount := range discount.Modules {
		if moduleDiscount.Module == "" {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "module is required")
		}
		if _, exists := modules[moduleDiscount.Module]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "duplicate module %q", moduleDiscount.Module)
		}
		modules[moduleDiscount.Module] = struct{}{}
		for _, feeDiscount := range moduleDiscount.Discounts {
			if err := ValidateFeeDiscount(feeDiscount); err != nil {
				return err
			}
		}
	}

	return nil
}
