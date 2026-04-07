package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

const globalDiscountStoreKey = "__global__"

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

	// authority defines the default moderatpr address
	authority string
}

func NewKeeper(
	cdc codec.BinaryCodec,
	key storetypes.StoreKey,
	transientKey storetypes.StoreKey,
	moduleKeepers map[string]types.ModuleKeeper,
	authority string,
) Keeper {
	return Keeper{
		cdc:           cdc,
		storeKey:      key,
		transientKey:  transientKey,
		moduleKeepers: moduleKeepers,
		authority:     authority,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", types.ModuleName)
}

func (k Keeper) GetAuthority() string {
	return k.authority
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
	discountStore := prefix.NewStore(store, types.KeyDiscounts)

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

	bz := discountStore.Get(discountStoreKey(accStr))
	if bz == nil {
		return types.AccountDiscount{}, false
	}

	accDiscount := types.AccountDiscount{}
	k.cdc.MustUnmarshal(bz, &accDiscount)

	return accDiscount, true
}

func (k Keeper) GetModuleDiscounts(ctx sdk.Context, accStr, module string) ([]types.Discount, bool) {
	accDiscount, ok := k.GetAccountDiscounts(ctx, accStr)
	if !ok {
		return nil, false
	}

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
	discountStore.Set(discountStoreKey(discount.Address), k.cdc.MustMarshal(&discount))
}

func (k Keeper) DeleteAccountDiscounts(ctx sdk.Context, accStr string) {
	store := ctx.KVStore(k.storeKey)
	discountStore := prefix.NewStore(store, types.KeyDiscounts)
	discountStore.Delete(discountStoreKey(accStr))
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

	for i := range discounts.Modules {
		for j, discount := range discounts.Modules[i].Discounts {
			if discount.MsgType == msgType {
				discounts.Modules[i].Discounts = append(discounts.Modules[i].Discounts[:j], discounts.Modules[i].Discounts[j+1:]...)
				break
			}
		}
	}

	k.SetAccountDiscounts(ctx, discounts)
}

func (k Keeper) ResolveDiscount(ctx sdk.Context, feePayerAddr string, msgs []sdk.Msg) types.Discount {
	// Never apply discounts to multi-message txs.
	if len(msgs) != 1 {
		return types.Discount{}
	}

	discount, module, ok := k.getMatchedDiscount(ctx, feePayerAddr, msgs)
	if ok {
		return k.getDiscountWithModuleCheck(ctx, discount, module, msgs)
	}

	discount, module, ok = k.getMatchedDiscount(ctx, "", msgs)
	if ok {
		return k.getDiscountWithModuleCheck(ctx, discount, module, msgs)
	}

	return types.Discount{}
}

// GetDiscount keeps backward compatibility and proxies to ResolveDiscount.
func (k Keeper) GetDiscount(ctx sdk.Context, feePayerAddr string, msgs []sdk.Msg) types.Discount {
	return k.ResolveDiscount(ctx, feePayerAddr, msgs)
}

func (k Keeper) getDiscountWithModuleCheck(ctx sdk.Context, discount types.Discount, module string, msgs []sdk.Msg) types.Discount {
	if k.moduleKeepers[module] == nil {
		return discount
	}
	if k.moduleKeepers[module].CheckDiscount(ctx, discount, msgs) {
		return discount
	}

	return types.Discount{}
}

func (k Keeper) getMatchedDiscount(ctx sdk.Context, accStr string, msgs []sdk.Msg) (types.Discount, string, bool) {
	accDiscount, ok := k.GetAccountDiscounts(ctx, accStr)
	if !ok || len(msgs) == 0 {
		return types.Discount{}, "", false
	}

	msgTypeURL := sdk.MsgTypeURL(msgs[0])
	for _, moduleDiscount := range accDiscount.Modules {
		for _, d := range moduleDiscount.Discounts {
			if d.MsgType == msgTypeURL {
				return d, moduleDiscount.Module, true
			}
		}
	}

	return types.Discount{}, "", false
}

func discountStoreKey(address string) []byte {
	if address == "" {
		return []byte(globalDiscountStoreKey)
	}
	return []byte(address)
}
