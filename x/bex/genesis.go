package bex

import (
	"fmt"

	"github.com/GPTx-global/guru-v2/x/bex/keeper"
	"github.com/GPTx-global/guru-v2/x/bex/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// InitGenesis new bex genesis
func InitGenesis(ctx sdk.Context, keeper keeper.Keeper, data types.GenesisState) {
	if data.ModeratorAddress == "" {
		data.ModeratorAddress = keeper.GetAuthority()
	}
	keeper.SetModeratorAddress(ctx, data.ModeratorAddress)
	for _, exchange := range data.Exchanges {
		keeper.SetExchange(ctx, &exchange)
	}
	keeper.SetRatemeter(ctx, &data.Ratemeter)
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func ExportGenesis(ctx sdk.Context, keeper keeper.Keeper) types.GenesisState {
	moderator_address := keeper.GetModeratorAddress(ctx)
	exchanges, _, err := keeper.GetPaginatedExchanges(ctx, &query.PageRequest{Limit: query.PaginationMaxLimit})
	if err != nil {
		panic(fmt.Errorf("unable to fetch exchanges %v", err))
	}
	ratemeter, err := keeper.GetRatemeter(ctx)
	if err != nil {
		panic(fmt.Errorf("unable to fetch ratemeter %v", err))
	}
	return types.NewGenesisState(moderator_address, *ratemeter, exchanges)
}
