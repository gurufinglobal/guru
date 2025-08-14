package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"
	"github.com/GPTx-global/guru-v2/x/feepolicy/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// Keeper of the xmsquare store
type Keeper struct {
	// Protobuf codec
	cdc codec.BinaryCodec

	// Store key required for the Fee Market Prefix KVStore.
	storeKey storetypes.StoreKey

	// key to access the transient store, which is reset on every block during Commit
	transientKey storetypes.StoreKey

	// module keepers
	moduleKeepers map[string]types.ModuleKeeper
}

func NewKeeper(
	cdc codec.BinaryCodec,
	key storetypes.StoreKey,
	transientKey storetypes.StoreKey,
	moduleKeepers map[string]types.ModuleKeeper,
) Keeper {

	return Keeper{
		cdc:           cdc,
		storeKey:      key,
		transientKey:  transientKey,
		moduleKeepers: moduleKeepers,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", types.ModuleName)
}

// GetModeratorAddress returns the current moderator address.
func (k Keeper) GetModeratorAddress(ctx sdk.Context) (types.Moderator, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyModeratorAddress)
	if bz == nil {
		return types.Moderator{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "moderator address not found")
	}
	moderator := types.Moderator{}
	k.cdc.MustUnmarshal(bz, &moderator)
	return moderator, nil
}

// SetModeratorAddress adds/updates the moderator address.
func (k Keeper) SetModeratorAddress(ctx sdk.Context, moderator types.Moderator) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.KeyModeratorAddress, k.cdc.MustMarshal(&moderator))
}

func (k Keeper) GetPaginatedDiscounts(ctx sdk.Context, pagination *query.PageRequest) ([]types.AccountDiscount, *query.PageResponse, error) {
	store := ctx.KVStore(k.storeKey)
	discountStore := prefix.NewStore(store, []byte(types.KeyDiscounts))

	discounts := []types.AccountDiscount{}

	pageRes, err := query.Paginate(discountStore, pagination, func(key, value []byte) error {
		discount := types.AccountDiscount{}
		k.cdc.MustUnmarshal(value, &discount)
		discounts = append(discounts, discount)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return discounts, pageRes, nil
}

func (k Keeper) GetAccountDiscounts(ctx sdk.Context, accStr string) (types.AccountDiscount, bool) {
	store := ctx.KVStore(k.storeKey)
	discountStore := prefix.NewStore(store, types.KeyDiscounts)

	bz := discountStore.Get([]byte(accStr))
	if bz == nil {
		return types.AccountDiscount{}, false
	}

	accDiscount := types.AccountDiscount{}
	k.cdc.MustUnmarshal(bz, &accDiscount)

	return accDiscount, true
}

func (k Keeper) GetModuleDiscounts(ctx sdk.Context, accStr, module string) ([]types.Discount, bool) {
	store := ctx.KVStore(k.storeKey)
	discountStore := prefix.NewStore(store, types.KeyDiscounts)

	bz := discountStore.Get([]byte(accStr))
	if bz == nil {
		return nil, false
	}

	accDiscount := types.AccountDiscount{}
	k.cdc.MustUnmarshal(bz, &accDiscount)

	for _, discount := range accDiscount.Modules {
		if discount.Module == module {
			return discount.Discounts, true
		}
	}

	return nil, false
}

func (k Keeper) SetAccountDiscounts(ctx sdk.Context, discount types.AccountDiscount) {
	store := ctx.KVStore(k.storeKey)
	discountStore := prefix.NewStore(store, types.KeyDiscounts)
	discountStore.Set([]byte(discount.Address), k.cdc.MustMarshal(&discount))
}

func (k Keeper) DeleteAccountDiscounts(ctx sdk.Context, accStr string) {
	store := ctx.KVStore(k.storeKey)
	discountStore := prefix.NewStore(store, types.KeyDiscounts)
	discountStore.Delete([]byte(accStr))
}

func (k Keeper) DeleteModuleDiscounts(ctx sdk.Context, accStr, module string) {
	discounts, ok := k.GetAccountDiscounts(ctx, accStr)
	if !ok {
		return
	}

	for i, moduleDiscount := range discounts.Modules {
		if moduleDiscount.Module == module {
			discounts.Modules = append(discounts.Modules[:i], discounts.Modules[i+1:]...)
			break
		}
	}

	k.SetAccountDiscounts(ctx, discounts)
}

func (k Keeper) DeleteMsgTypeDiscounts(ctx sdk.Context, accStr, msgType string) {
	discounts, ok := k.GetAccountDiscounts(ctx, accStr)
	if !ok {
		return
	}

	for _, moduleDiscount := range discounts.Modules {
		for j, discount := range moduleDiscount.Discounts {
			if discount.MsgType == msgType {
				moduleDiscount.Discounts = append(moduleDiscount.Discounts[:j], moduleDiscount.Discounts[j+1:]...)
				break
			}
		}
	}

	k.SetAccountDiscounts(ctx, discounts)
}

func (k Keeper) GetDiscount(ctx sdk.Context, feePayerAddr string, msgs []sdk.Msg) types.Discount {

	accDiscount, ok := k.GetAccountDiscounts(ctx, feePayerAddr)
	if !ok {
		return types.Discount{}
	}

	discount := types.Discount{}
	module := ""
	for _, m := range msgs {
		t := sdk.MsgTypeURL(m) // converts from (ex.) *types.MsgRecvPacket to "/ibc.core.channel.v1.MsgRecvPacket"
		for _, moduleDiscount := range accDiscount.Modules {
			for _, d := range moduleDiscount.Discounts {
				if d.MsgType == t {
					discount = d
					module = moduleDiscount.Module
					break
				}
			}
		}
	}

	// check if the module has additional checker layers
	if k.moduleKeepers[module] == nil {
		return discount
	} else {
		if k.moduleKeepers[module].CheckDiscount(ctx, discount, msgs) {
			return discount
		}
	}

	return types.Discount{}
}
