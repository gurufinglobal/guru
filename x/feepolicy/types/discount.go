package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	FeeDiscountTypePercent = "percent"
	FeeDiscountTypeFixed   = "fixed"
)

// ValidateFeeDiscount validates a fee discount
// Discount is the basic type for registering a single discount
func ValidateFeeDiscount(discount Discount) error {
	if discount.DiscountType != FeeDiscountTypePercent && discount.DiscountType != FeeDiscountTypeFixed {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid discount type. accepted types are: "+FeeDiscountTypePercent+", "+FeeDiscountTypeFixed)
	}

	if discount.MsgType == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "msg type is required")
	}

	if discount.Amount.IsNegative() || discount.Amount.IsZero() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "discount value must be greater than 0")
	}
	if discount.DiscountType == FeeDiscountTypePercent && discount.Amount.GT(math.LegacyNewDec(100)) {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "percent discount must be less than or equal to 100")
	}

	return nil
}

// ValidateAccountDiscount validates an account discount
// AccountDiscount contains all discounts for a single account
func ValidateAccountDiscount(discount AccountDiscount) error {
	// Empty address is reserved for global discount policy.
	if discount.Address != "" {
		_, err := sdk.AccAddressFromBech32(discount.Address)
		if err != nil {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "string could not be parsed as address: %v", err)
		}
	}

	for _, moduleDiscount := range discount.Modules {
		if moduleDiscount.Module == "" {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "module is required")
		}

		for _, discount := range moduleDiscount.Discounts {
			if err := ValidateFeeDiscount(discount); err != nil {
				return err
			}
		}
	}

	return nil
}
