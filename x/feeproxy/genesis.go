package feeproxy

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/keeper"
	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

// InitGenesis initializes x/feeproxy state from genesis.
// If genesis does not provide params, DefaultParams() is used.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, data types.GenesisState) {
	moderator := data.ModeratorAddress
	if moderator == "" {
		moderator = k.GetAuthority()
	}

	if err := k.SetModeratorAddress(ctx, moderator); err != nil {
		panic(fmt.Errorf("failed to set feeproxy moderator_address: %w", err))
	}

	params := types.DefaultParams()
	if data.Params != nil {
		params = *data.Params
	}

	// If genesis didn't specify these, default them to the moderator.
	// This keeps the module operable even if genesis only provides moderator_address.
	if params.AdminAddress == "" {
		params.AdminAddress = moderator
	}
	if params.ReserveAddress == "" {
		params.ReserveAddress = params.AdminAddress
	}

	if err := params.Validate(); err != nil {
		panic(fmt.Errorf("invalid feeproxy params: %w", err))
	}

	if err := k.SetParams(ctx, params); err != nil {
		panic(fmt.Errorf("failed to set feeproxy params: %w", err))
	}
}

// ExportGenesis exports x/feeproxy state to genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) types.GenesisState {
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		panic(err)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}

	return *types.NewGenesisState(moderator, &params)
}
