package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

// InitGenesis initializes the ibc-transfer state and binds to PortID.
func (k Keeper) InitGenesis(ctx sdk.Context, state transwapv1.GenesisState) {
	k.SetPort(ctx, state.PortId)

	for _, denom := range state.Denoms {
		k.SetDenom(ctx, denom)
		k.SetDenomMetadata(ctx, denom)
	}

	// Every denom will have only one total escrow amount, since any
	// duplicate entry will fail validation in Validate of GenesisState
	totalEscrowed, err := types.ProtoCoinsToSDK(state.TotalEscrowed)
	if err != nil {
		panic(err)
	}
	for _, denomEscrow := range totalEscrowed {
		k.SetTotalEscrowForDenom(ctx, denomEscrow)
	}
}

// ExportGenesis exports ibc-transfer module's portID and denom trace info into its genesis state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *transwapv1.GenesisState {
	return &transwapv1.GenesisState{
		PortId:        k.GetPort(ctx),
		Denoms:        k.GetAllDenoms(ctx),
		TotalEscrowed: types.SDKCoinsToProto(k.GetAllTotalEscrowed(ctx)),
	}
}
