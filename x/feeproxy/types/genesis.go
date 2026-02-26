package types

import (
	"fmt"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewGenesisState creates a new GenesisState object.
func NewGenesisState(moderatorAddress string, params *Params) *GenesisState {
	return &GenesisState{
		ModeratorAddress: moderatorAddress,
		Params:           params,
	}
}

// DefaultGenesisState creates a default GenesisState object.
func DefaultGenesisState() *GenesisState {
	// IMPORTANT:
	// We intentionally omit params from default genesis. Admin/reserve addresses are chain-specific,
	// and InitGenesis will populate missing addresses using the keeper authority / moderator_address.
	return &GenesisState{}
}

func (gs GenesisState) Validate() error {
	// moderator_address can be omitted in genesis; InitGenesis will default it.
	if gs.ModeratorAddress != "" {
		if _, err := sdk.AccAddressFromBech32(gs.ModeratorAddress); err != nil {
			return fmt.Errorf("invalid moderator_address: %w", err)
		}
	}

	// params can be omitted in genesis; InitGenesis will default it.
	// Also allow admin/reserve to be omitted in genesis params: InitGenesis will fill them
	// from moderator_address.
	if gs.Params != nil {
		if gs.Params.FeePercentage.IsNil() {
			return fmt.Errorf("invalid params: fee_percentage is nil")
		}
		if gs.Params.FeePercentage.IsNegative() {
			return fmt.Errorf("invalid params: fee_percentage cannot be negative: %s", gs.Params.FeePercentage)
		}
		if gs.Params.FeePercentage.GT(sdkmath.LegacyOneDec()) {
			return fmt.Errorf("invalid params: fee_percentage cannot be greater than 1: %s", gs.Params.FeePercentage)
		}

		if gs.Params.AdminAddress != "" {
			if _, err := sdk.AccAddressFromBech32(gs.Params.AdminAddress); err != nil {
				return fmt.Errorf("invalid params: invalid admin_address: %w", err)
			}
		}
		if gs.Params.ReserveAddress != "" {
			if _, err := sdk.AccAddressFromBech32(gs.Params.ReserveAddress); err != nil {
				return fmt.Errorf("invalid params: invalid reserve_address: %w", err)
			}
		}
	}
	return nil
}
