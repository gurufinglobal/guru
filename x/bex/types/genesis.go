package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(moderator_address string, ratemeter Ratemeter, exchanges []Exchange) GenesisState {
	return GenesisState{
		ModeratorAddress: moderator_address,
		Ratemeter:        ratemeter,
		Exchanges:        exchanges,
	}
}

// DefaultGenesisState creates a default GenesisState object
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		ModeratorAddress: "",
		Ratemeter:        DefaultRatemeter(),
		Exchanges:        []Exchange{},
	}
}

// Validate validates the genesis state to ensure the
// expected invariants holds.
func (gs GenesisState) Validate() error {
	// if err := validateAddress(gs.ModeratorAddress); err != nil {
	// 	return err
	// }

	for _, exchange := range gs.Exchanges {
		if err := exchange.Validate(); err != nil {
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
