package keeper

import (
	"context"

	"cosmossdk.io/log/v2"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	"github.com/gurufinglobal/guru/v3/x/constitution/types"
)

type Keeper struct {
	authority sdk.AccAddress

	params collections.Item[*constitutionv1.Params]

	schema collections.Schema
}

func NewKeeper(
	authority sdk.AccAddress,
	storeService store.KVStoreService,
) Keeper {
	k := Keeper{
		authority: authority,
	}

	sb := collections.NewSchemaBuilder(storeService)

	k.params = collections.NewItem(sb, types.ParamsKey, "params", codec.CollValueV2[constitutionv1.Params]())
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.schema = schema

	return k
}

func (k Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+types.ModuleName)
}
