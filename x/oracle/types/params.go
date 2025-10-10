package types

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
)

// DefaultParams returns default oracle module parameters
func DefaultParams() Params {
	return Params{
		EnableOracle:          true,
		SubmitWindow:          3600, // 1 hour in seconds
		MinSubmitPerWindow:    sdkmath.LegacyNewDec(1),
		SlashFractionDowntime: sdkmath.LegacyNewDecWithPrec(1, 2), // 1%
		MaxAccountListSize:    100,                                // Maximum 100 accounts in account list (also max submissions) - for client validation
	}
}

// Validate performs basic validation on oracle parameters
func (p Params) Validate() error {
	if p.SubmitWindow == 0 {
		return fmt.Errorf("submit window cannot be zero")
	}

	if p.MinSubmitPerWindow.IsNegative() {
		return fmt.Errorf("min submit per window cannot be negative")
	}

	if p.SlashFractionDowntime.IsNegative() {
		return fmt.Errorf("slash fraction downtime cannot be negative")
	}

	if p.MaxAccountListSize == 0 {
		return fmt.Errorf("max account list size cannot be zero")
	}

	if p.MaxAccountListSize > 100 {
		return fmt.Errorf("max account list size cannot exceed 100")
	}

	return nil
}
