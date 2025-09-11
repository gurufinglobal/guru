package feepolicy

import (
	"fmt"
	"math"

	"github.com/GPTx-global/guru-v2/v2/x/feepolicy/keeper"
	"github.com/GPTx-global/guru-v2/v2/x/feepolicy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// InitGenesis new feepolicy genesis
func InitGenesis(ctx sdk.Context, keeper keeper.Keeper, data types.GenesisState) {
	if data.ModeratorAddress == "" {
		data.ModeratorAddress = keeper.GetAuthority()
	}

	if _, err := sdk.AccAddressFromBech32(data.ModeratorAddress); err != nil {
		panic(fmt.Sprintf("invalid moderator address: %s. Error: %s", data.ModeratorAddress, err))
	}

	keeper.SetModeratorAddress(ctx, types.Moderator{Address: data.ModeratorAddress})
	for _, discount := range data.Discounts {
		keeper.SetAccountDiscounts(ctx, discount)
	}
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func ExportGenesis(ctx sdk.Context, keeper keeper.Keeper) types.GenesisState {
	moderator, err := keeper.GetModeratorAddress(ctx)
	if err != nil {
		panic(err)
	}
	discounts, _, err := keeper.GetPaginatedDiscounts(ctx, &query.PageRequest{Limit: math.MaxUint64, CountTotal: true})
	if err != nil {
		panic(fmt.Errorf("unable to fetch discounts %v", err))
	}
	return types.NewGenesisState(moderator.Address, discounts)
}
