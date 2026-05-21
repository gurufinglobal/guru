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

	stakingKeeper types.StakingKeeper

	params            collections.Item[*constitutionv1.Params]
	changedValidators collections.KeySet[[]byte]
	enforceAllBonded  collections.Item[bool]

	schema collections.Schema
}

func NewKeeper(authority sdk.AccAddress, stakingKeeper types.StakingKeeper, storeService store.KVStoreService) Keeper {
	k := Keeper{
		authority:     authority,
		stakingKeeper: stakingKeeper,
	}

	sb := collections.NewSchemaBuilder(storeService)

	k.params = collections.NewItem(sb, types.ParamsKey, "params", codec.CollValueV2[constitutionv1.Params]())
	k.changedValidators = collections.NewKeySet(sb, types.ChangedValidatorsKey, "changed_validators", collections.BytesKey)
	k.enforceAllBonded = collections.NewItem(sb, types.EnforceAllBondedKey, "enforce_all_bonded", collections.BoolValue)
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
