package oracle

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/y/oracle/keeper"
	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

// InitGenesis initializes the oracle module's state from a provided genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, gs types.GenesisState) {
	if err := gs.Validate(); err != nil {
		panic(errorsmod.Wrap(err, "invalid oracle genesis"))
	}

	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(errorsmod.Wrap(err, "failed to set params"))
	}

	moderator := gs.ModeratorAddress
	if moderator == "" {
		moderator = k.GetAuthority()
	}
	if _, err := sdk.AccAddressFromBech32(moderator); err != nil {
		panic(errorsmod.Wrap(err, "invalid moderator address"))
	}
	k.SetModeratorAddress(ctx, moderator)

	var maxID uint64
	for _, req := range gs.Requests {
		k.SetRequest(ctx, req)
		if req.Id > maxID {
			maxID = req.Id
		}
	}
	k.SetRequestCount(ctx, maxID)

	for _, addr := range gs.WhitelistAddresses {
		k.AddWhitelistAddress(ctx, addr)
	}

	for _, cat := range gs.Categories {
		k.SetCategory(ctx, cat)
	}
}

// ExportGenesis returns the oracle module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	params := k.GetParams(ctx)
	moderator := k.GetModeratorAddress(ctx)

	var requests []types.OracleRequest
	k.IterateRequests(ctx, func(req types.OracleRequest) bool {
		requests = append(requests, req)
		return false
	})

	return &types.GenesisState{
		Params:             params,
		ModeratorAddress:   moderator,
		Requests:           requests,
		Categories:         k.GetCategories(ctx),
		WhitelistAddresses: k.GetWhitelist(ctx),
	}
}
