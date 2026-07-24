package keeper

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/ethereum/go-ethereum/common"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

const globalDiscountStoreKey = "__global__"

// Keeper owns discount state. Constitution is the sole moderator state owner;
// the legacy feepolicy 0x01 moderator prefix is reserved and ignored, while
// discount prefix 0x02 and its value codec preserve the v2 raw layout.
type Keeper struct {
	accountCodec       address.Codec
	constitutionKeeper ConstitutionKeeper
	moduleKeepers      map[string]types.ModuleKeeper

	discounts collections.Map[string, types.AccountDiscount]
	schema    collections.Schema
}

func NewKeeper(
	storeService store.KVStoreService,
	cdc codec.Codec,
	accountCodec address.Codec,
	moduleKeepers map[string]types.ModuleKeeper,
	constitutionKeeper ConstitutionKeeper,
) Keeper {
	if storeService == nil {
		panic("feepolicy store service cannot be nil")
	}
	if cdc == nil {
		panic("feepolicy codec cannot be nil")
	}
	if accountCodec == nil {
		panic("feepolicy account codec cannot be nil")
	}
	if constitutionKeeper == nil {
		panic("feepolicy constitution keeper cannot be nil")
	}

	keepers := make(map[string]types.ModuleKeeper, len(moduleKeepers))
	for module, moduleKeeper := range moduleKeepers {
		keepers[module] = moduleKeeper
	}

	schemaBuilder := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		accountCodec:       accountCodec,
		constitutionKeeper: constitutionKeeper,
		moduleKeepers:      keepers,
		discounts: collections.NewMap(
			schemaBuilder,
			types.DiscountsKey,
			"discounts",
			collections.StringKey,
			codec.CollValue[types.AccountDiscount](cdc),
		),
	}

	schema, err := schemaBuilder.Build()
	if err != nil {
		panic(err)
	}
	k.schema = schema

	return k
}

func (k Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+types.ModuleName)
}

// CanonicalAddress validates a Hex/Bech32 discount-account address and returns
// the chain's canonical Bech32 representation. Discount accounts are EVM
// accounts and therefore exactly 20 bytes.
func (k Keeper) CanonicalAddress(value string) (string, error) {
	canonical, _, err := k.canonicalAddress(value)
	return canonical, err
}

// CanonicalModeratorAddress follows Constitution/BEX address semantics. A
// moderator is an SDK signer, not a discount-account key, so the injected
// address codec owns its accepted byte lengths.
func (k Keeper) CanonicalModeratorAddress(value string) (string, error) {
	canonical, _, err := k.canonicalModeratorAddress(value)
	return canonical, err
}

func (k Keeper) GetModeratorAddress(ctx context.Context) (string, error) {
	moderator, err := k.constitutionKeeper.GetModeratorAddress(ctx)
	if err != nil {
		return "", err
	}
	canonical, _, err := k.canonicalModeratorAddress(moderator)
	if err != nil {
		return "", types.ErrWrongModerator.Wrapf("invalid configured Constitution moderator address: %v", err)
	}
	return canonical, nil
}

// NormalizeAccountDiscount validates and deep-copies a policy, converting a
// non-empty Hex/Bech32 address to the one canonical Bech32 representation.
func (k Keeper) NormalizeAccountDiscount(discount types.AccountDiscount) (types.AccountDiscount, error) {
	if err := types.ValidateAccountDiscount(discount); err != nil {
		return types.AccountDiscount{}, err
	}

	normalized := cloneAccountDiscount(discount)
	if discount.Address == "" {
		normalized.Address = ""
		return normalized, nil
	}

	canonical, _, err := k.canonicalAddress(discount.Address)
	if err != nil {
		return types.AccountDiscount{}, err
	}
	normalized.Address = canonical
	return normalized, nil
}

func (k Keeper) GetPaginatedDiscounts(
	ctx context.Context,
	pagination *query.PageRequest,
) ([]types.AccountDiscount, *query.PageResponse, error) {
	return query.CollectionPaginate(
		ctx,
		k.discounts,
		pagination,
		func(_ string, discount types.AccountDiscount) (types.AccountDiscount, error) {
			return discount, nil
		},
	)
}

func (k Keeper) GetAllDiscounts(ctx context.Context) ([]types.AccountDiscount, error) {
	discounts := make([]types.AccountDiscount, 0)
	err := k.discounts.Walk(ctx, nil, func(_ string, discount types.AccountDiscount) (bool, error) {
		discounts = append(discounts, discount)
		return false, nil
	})
	return discounts, err
}

func (k Keeper) GetAccountDiscounts(ctx context.Context, account string) (types.AccountDiscount, bool, error) {
	key, err := k.discountKey(account)
	if err != nil {
		return types.AccountDiscount{}, false, err
	}

	discount, err := k.discounts.Get(ctx, key)
	if errors.Is(err, collections.ErrNotFound) {
		return types.AccountDiscount{}, false, nil
	}
	if err != nil {
		return types.AccountDiscount{}, false, err
	}
	return discount, true, nil
}

func (k Keeper) GetModuleDiscounts(ctx context.Context, account, module string) ([]types.Discount, bool, error) {
	accountDiscount, found, err := k.GetAccountDiscounts(ctx, account)
	if err != nil || !found {
		return nil, false, err
	}
	for _, moduleDiscount := range accountDiscount.Modules {
		if moduleDiscount.Module == module {
			return append([]types.Discount(nil), moduleDiscount.Discounts...), true, nil
		}
	}
	return nil, false, nil
}

// SetAccountDiscounts replaces the complete account/global record.
func (k Keeper) SetAccountDiscounts(ctx context.Context, discount types.AccountDiscount) error {
	normalized, err := k.NormalizeAccountDiscount(discount)
	if err != nil {
		return err
	}
	key, err := k.discountKey(normalized.Address)
	if err != nil {
		return err
	}
	return k.discounts.Set(ctx, key, normalized)
}

func (k Keeper) DeleteAccountDiscounts(ctx context.Context, account string) error {
	key, err := k.discountKey(account)
	if err != nil {
		return err
	}
	return k.discounts.Remove(ctx, key)
}

func (k Keeper) DeleteModuleDiscounts(ctx context.Context, account, module string) error {
	discount, found, err := k.GetAccountDiscounts(ctx, account)
	if err != nil || !found {
		return err
	}
	filtered := discount.Modules[:0]
	for _, moduleDiscount := range discount.Modules {
		if moduleDiscount.Module == module {
			continue
		}
		filtered = append(filtered, moduleDiscount)
	}
	discount.Modules = filtered
	return k.SetAccountDiscounts(ctx, discount)
}

// DeleteMsgTypeDiscounts intentionally preserves the v2 behavior: module is
// not part of this operation, and the first matching msg type in every module
// is removed.
func (k Keeper) DeleteMsgTypeDiscounts(ctx context.Context, account, msgType string) error {
	discount, found, err := k.GetAccountDiscounts(ctx, account)
	if err != nil || !found {
		return err
	}
	for i := range discount.Modules {
		for j, feeDiscount := range discount.Modules[i].Discounts {
			if feeDiscount.MsgType == msgType {
				discount.Modules[i].Discounts = append(
					discount.Modules[i].Discounts[:j],
					discount.Modules[i].Discounts[j+1:]...,
				)
				break
			}
		}
	}
	return k.SetAccountDiscounts(ctx, discount)
}

// ResolveDiscount selects a policy only for a transaction with exactly one
// top-level message. State decoding failures are returned to keep ante
// processing fail-closed.
func (k Keeper) ResolveDiscount(
	ctx context.Context,
	feePayerAddress string,
	msgs []sdk.Msg,
) (types.Discount, error) {
	if len(msgs) != 1 {
		return types.Discount{}, nil
	}

	canonicalPayer, _, err := k.canonicalAddress(feePayerAddress)
	if err != nil {
		return types.Discount{}, err
	}

	discount, module, matched, err := k.getMatchedDiscount(ctx, canonicalPayer, msgs[0])
	if err != nil {
		return types.Discount{}, err
	}
	if matched {
		return k.applyModuleCheck(ctx, module, discount, msgs), nil
	}

	discount, module, matched, err = k.getMatchedDiscount(ctx, "", msgs[0])
	if err != nil {
		return types.Discount{}, err
	}
	if matched {
		return k.applyModuleCheck(ctx, module, discount, msgs), nil
	}

	return types.Discount{}, nil
}

// GetDiscount is retained as an alias for callers migrating from v2.
func (k Keeper) GetDiscount(ctx context.Context, feePayerAddress string, msgs []sdk.Msg) (types.Discount, error) {
	return k.ResolveDiscount(ctx, feePayerAddress, msgs)
}

func (k Keeper) getMatchedDiscount(
	ctx context.Context,
	account string,
	msg sdk.Msg,
) (types.Discount, string, bool, error) {
	accountDiscount, found, err := k.GetAccountDiscounts(ctx, account)
	if err != nil || !found {
		return types.Discount{}, "", false, err
	}

	msgTypeURL := sdk.MsgTypeURL(msg)
	for _, moduleDiscount := range accountDiscount.Modules {
		for _, discount := range moduleDiscount.Discounts {
			if discount.MsgType == msgTypeURL {
				return discount, moduleDiscount.Module, true, nil
			}
		}
	}
	return types.Discount{}, "", false, nil
}

func (k Keeper) applyModuleCheck(
	ctx context.Context,
	module string,
	discount types.Discount,
	msgs []sdk.Msg,
) types.Discount {
	checker := k.moduleKeepers[module]
	if checker == nil || checker.CheckDiscount(ctx, discount, msgs) {
		return discount
	}
	return types.Discount{}
}

func (k Keeper) authorizeModerator(ctx context.Context, candidate string) (string, error) {
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return "", types.ErrWrongModerator.Wrap("constitution moderator_address is not initialized")
		}
		return "", err
	}
	expectedCanonical, expectedBytes, err := k.canonicalModeratorAddress(moderator)
	if err != nil {
		return "", types.ErrWrongModerator.Wrapf("invalid configured Constitution moderator address: %v", err)
	}
	_, candidateBytes, err := k.canonicalModeratorAddress(candidate)
	if err != nil {
		return "", types.ErrWrongModerator.Wrapf("invalid moderator address: %v", err)
	}
	if !bytes.Equal(expectedBytes, candidateBytes) {
		return "", types.ErrWrongModerator.Wrapf("expected %s, got %s", expectedCanonical, candidate)
	}
	return expectedCanonical, nil
}

func (k Keeper) discountKey(account string) (string, error) {
	if account == "" {
		return globalDiscountStoreKey, nil
	}
	canonical, _, err := k.canonicalAddress(account)
	return canonical, err
}

func (k Keeper) canonicalAddress(value string) (string, []byte, error) {
	canonical, decoded, err := k.canonicalModeratorAddress(value)
	if err != nil {
		return "", nil, err
	}
	if len(decoded) != common.AddressLength {
		return "", nil, sdkerrors.ErrInvalidAddress.Wrapf("address must be %d bytes, got %d", common.AddressLength, len(decoded))
	}
	return canonical, decoded, nil
}

func (k Keeper) canonicalModeratorAddress(value string) (string, []byte, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", nil, sdkerrors.ErrInvalidAddress.Wrap("address cannot be empty or contain surrounding whitespace")
	}
	decoded, err := k.accountCodec.StringToBytes(value)
	if err != nil {
		return "", nil, sdkerrors.ErrInvalidAddress.Wrap(err.Error())
	}
	canonical, err := k.accountCodec.BytesToString(decoded)
	if err != nil {
		return "", nil, sdkerrors.ErrInvalidAddress.Wrap(err.Error())
	}
	return canonical, decoded, nil
}

func cloneAccountDiscount(discount types.AccountDiscount) types.AccountDiscount {
	cloned := types.AccountDiscount{
		Address: discount.Address,
		Modules: make([]types.ModuleDiscount, len(discount.Modules)),
	}
	for i, moduleDiscount := range discount.Modules {
		cloned.Modules[i] = types.ModuleDiscount{
			Module:    moduleDiscount.Module,
			Discounts: make([]types.Discount, len(moduleDiscount.Discounts)),
		}
		for j, feeDiscount := range moduleDiscount.Discounts {
			cloned.Modules[i].Discounts[j] = feeDiscount
			if !feeDiscount.Amount.IsNil() {
				cloned.Modules[i].Discounts[j].Amount = feeDiscount.Amount.Clone()
			}
		}
	}
	return cloned
}
