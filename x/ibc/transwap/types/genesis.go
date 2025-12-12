package types

import (
	host "github.com/cosmos/ibc-go/v10/modules/core/24-host"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewGenesisState creates a new ibc-transfer GenesisState instance.
func NewGenesisState(portID string, denoms Denoms, totalEscrowed sdk.Coins) *GenesisState {
	return &GenesisState{
		PortId:        portID,
		Denoms:        denoms,
		TotalEscrowed: totalEscrowed,
	}
}

// DefaultGenesisState returns a GenesisState with "transfer" as the default PortID.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		PortId:        PortID,
		Denoms:        Denoms{},
		TotalEscrowed: sdk.Coins{},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	if err := host.PortIdentifierValidator(gs.PortId); err != nil {
		return err
	}
	if err := gs.Denoms.Validate(); err != nil {
		return err
	}
	return gs.TotalEscrowed.Validate() // will fail if there are duplicates for any denom
}
