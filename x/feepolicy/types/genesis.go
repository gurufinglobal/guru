package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(moderator_address string, discounts []AccountDiscount) GenesisState {
	return GenesisState{
		ModeratorAddress: moderator_address,
		Discounts:        discounts,
	}
}

// DefaultGenesisState creates a default GenesisState object
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		ModeratorAddress: "",
		Discounts:        []AccountDiscount{},
	}
}

// Validate validates the genesis state to ensure the
// expected invariants holds.
func (gs GenesisState) Validate() error {
	if err := validateAddress(gs.ModeratorAddress); err != nil {
		return err
	}

	for _, discount := range gs.Discounts {
		if err := ValidateAccountDiscount(discount); err != nil {
			return err
		}
	}

	return nil
}

// method validates the address for genesis state
func validateAddress(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidType, " %+v", i.(string))
	}

	_, err := sdk.AccAddressFromBech32(v)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " %+v", err)
	}

	return nil
}
