package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

const allInvariantsRoute = "all"

func RegisterInvariants(registry sdk.InvariantRegistry, k Keeper) {
	registry.RegisterRoute(types.ModuleName, allInvariantsRoute, AllInvariants(k))
}

func AllInvariants(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		if err := k.AssertInvariants(ctx); err != nil {
			return sdk.FormatInvariant(types.ModuleName, allInvariantsRoute, err.Error()), true
		}
		return sdk.FormatInvariant(types.ModuleName, allInvariantsRoute, "all invariants hold"), false
	}
}
