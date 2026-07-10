package types

import (
	host "github.com/cosmos/ibc-go/v11/modules/core/24-host"

	sdk "github.com/cosmos/cosmos-sdk/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
)

// NewGenesisState creates a new transwap GenesisState instance.
func NewGenesisState(portID string, denoms Denoms, totalEscrowed sdk.Coins) *transwapv1.GenesisState {
	return &transwapv1.GenesisState{
		PortId:        portID,
		Denoms:        denoms,
		TotalEscrowed: SDKCoinsToProto(totalEscrowed),
	}
}

// DefaultGenesisState returns a GenesisState with transwap as the default PortID.
func DefaultGenesisState() *transwapv1.GenesisState {
	return &transwapv1.GenesisState{
		PortId:        PortID,
		Denoms:        Denoms{},
		TotalEscrowed: SDKCoinsToProto(sdk.Coins{}),
	}
}

// ValidateGenesisState performs basic genesis state validation.
func ValidateGenesisState(gs *transwapv1.GenesisState) error {
	if err := host.PortIdentifierValidator(gs.GetPortId()); err != nil {
		return err
	}
	if err := Denoms(gs.GetDenoms()).Validate(); err != nil {
		return err
	}
	totalEscrowed, err := ProtoCoinsToSDK(gs.GetTotalEscrowed())
	if err != nil {
		return err
	}
	return totalEscrowed.Validate()
}
