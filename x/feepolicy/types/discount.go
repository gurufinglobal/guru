package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	FeeDiscountTypePercent = "percent"
	FeeDiscountTypeFixed   = "fixed"
)

func ValidateFeeDiscount(discount Discount) error {
	if discount.DiscountType != FeeDiscountTypePercent && discount.DiscountType != FeeDiscountTypeFixed {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid discount type. accepted types are: "+FeeDiscountTypePercent+", "+FeeDiscountTypeFixed)
	}

	if discount.MsgType == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "msg type is required")
	}

	if discount.Amount.IsNegative() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "discount value must be greater than 0")
	}

	return nil
}

func ValidateAccountDiscount(discount AccountDiscount) error {
	_, err := sdk.AccAddressFromBech32(discount.Address)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "string could not be parsed as address: %v", err)
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
