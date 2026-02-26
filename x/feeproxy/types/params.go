package types

import (
	"fmt"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func DefaultParams() Params {
	// Default to 0 to avoid accidental fee charging until governance/genesis sets it.
	return Params{
		AdminAddress:   "",
		ReserveAddress: "",
		FeePercentage:  sdkmath.LegacyZeroDec(),
	}
}

func (p Params) Validate() error {
	if _, err := sdk.AccAddressFromBech32(p.AdminAddress); err != nil {
		return fmt.Errorf("invalid admin_address: %w", err)
	}
	if _, err := sdk.AccAddressFromBech32(p.ReserveAddress); err != nil {
		return fmt.Errorf("invalid reserve_address: %w", err)
	}

	if p.FeePercentage.IsNil() {
		return fmt.Errorf("invalid fee_percentage: nil")
	}
	if p.FeePercentage.IsNegative() {
		return fmt.Errorf("invalid fee_percentage: cannot be negative: %s", p.FeePercentage)
	}
	if p.FeePercentage.GT(sdkmath.LegacyOneDec()) {
		return fmt.Errorf("invalid fee_percentage: cannot be greater than 1: %s", p.FeePercentage)
	}
	return nil
}
