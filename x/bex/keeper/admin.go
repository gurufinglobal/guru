package keeper

import (
	"cosmossdk.io/store/prefix"
	"github.com/GPTx-global/guru-v2/x/bex/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

func (k Keeper) AddAdmin(ctx sdk.Context, adminAddr string) {
	store := ctx.KVStore(k.storeKey)
	adminStore := prefix.NewStore(store, types.KeyAdmins)

	adminStore.Set([]byte(adminAddr), []byte{1})
}

func (k Keeper) DeleteAdmin(ctx sdk.Context, adminAddr string) {
	store := ctx.KVStore(k.storeKey)
	adminStore := prefix.NewStore(store, types.KeyAdmins)

	adminStore.Delete([]byte(adminAddr))
}

func (k Keeper) IsAdminRegistered(ctx sdk.Context, adminAddr string) bool {
	store := ctx.KVStore(k.storeKey)
	adminStore := prefix.NewStore(store, types.KeyAdmins)

	bz := adminStore.Get([]byte(adminAddr))
	return len(bz) > 0 && bz[0] == 1
}

func (k Keeper) GetPaginatedAdmins(ctx sdk.Context, pagination *query.PageRequest) ([]string, *query.PageResponse, error) {
	store := ctx.KVStore(k.storeKey)
	adminStore := prefix.NewStore(store, types.KeyAdmins)

	admins := []string{}

	pageRes, err := query.Paginate(adminStore, pagination, func(key, value []byte) error {
		// add the admin to the list
		if len(value) > 0 && value[0] == 1 {
			admins = append(admins, string(key))
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return admins, pageRes, nil
}
