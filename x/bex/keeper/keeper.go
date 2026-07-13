package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

const DefaultNextExchangeID uint64 = 1

type volumeWindowKey = collections.Quad[uint64, uint32, uint64, uint32]

type Keeper struct {
	accountCodec       address.Codec
	accountKeeper      AccountKeeper
	bankKeeper         BankKeeper
	oracleKeeper       OracleKeeper
	constitutionKeeper ConstitutionKeeper
	channelKeeper      ChannelKeeper

	admins            collections.KeySet[string]
	exchanges         collections.Map[uint64, *bexv1.Exchange]
	exchangesByAdmin  collections.KeySet[collections.Pair[string, uint64]]
	reserveByAddress  collections.Map[string, uint64]
	nextExchangeID    collections.Sequence
	collectedFees     collections.Map[uint64, *bexv1.FeeLedger]
	lockedFees        collections.Map[uint64, *bexv1.FeeLedger]
	volumeWindow      collections.Map[volumeWindowKey, string]
	volumePruneCursor collections.Map[collections.Pair[uint64, uint32], uint64]

	schema collections.Schema
}

func NewKeeper(
	storeService store.KVStoreService,
	accountCodec address.Codec,
	accountKeeper AccountKeeper,
	bankKeeper BankKeeper,
	oracleKeeper OracleKeeper,
	constitutionKeeper ConstitutionKeeper,
	channelKeeper ChannelKeeper,
) Keeper {
	k := Keeper{
		accountCodec:       accountCodec,
		accountKeeper:      accountKeeper,
		bankKeeper:         bankKeeper,
		oracleKeeper:       oracleKeeper,
		constitutionKeeper: constitutionKeeper,
		channelKeeper:      channelKeeper,
	}

	sb := collections.NewSchemaBuilder(storeService)
	k.admins = collections.NewKeySet(sb, types.AdminsKey, "admins", collections.StringKey)
	k.exchanges = collections.NewMap(sb, types.ExchangesKey, "exchanges", collections.Uint64Key, codec.CollValueV2[bexv1.Exchange]())
	k.exchangesByAdmin = collections.NewKeySet(
		sb,
		types.ExchangesByAdminKey,
		"exchanges_by_admin",
		collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
	)
	k.reserveByAddress = collections.NewMap(sb, types.ReserveByAddressKey, "reserve_by_address", collections.StringKey, collections.Uint64Value)
	k.nextExchangeID = collections.NewSequence(sb, types.NextExchangeIDKey, "next_exchange_id")
	k.collectedFees = collections.NewMap(sb, types.CollectedFeesKey, "collected_fees", collections.Uint64Key, codec.CollValueV2[bexv1.FeeLedger]())
	k.lockedFees = collections.NewMap(sb, types.LockedFeesKey, "locked_fees", collections.Uint64Key, codec.CollValueV2[bexv1.FeeLedger]())
	k.volumeWindow = collections.NewMap(
		sb,
		types.VolumeWindowKey,
		"volume_window",
		collections.QuadKeyCodec(collections.Uint64Key, collections.Uint32Key, collections.Uint64Key, collections.Uint32Key),
		collections.StringValue,
	)
	k.volumePruneCursor = collections.NewMap(
		sb,
		types.VolumePruneCursorKey,
		"volume_prune_cursor",
		collections.PairKeyCodec(collections.Uint64Key, collections.Uint32Key),
		collections.Uint64Value,
	)

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

func ReserveModuleName(exchangeID uint64) string {
	return fmt.Sprintf("bex/reserve/%d", exchangeID)
}

func (k Keeper) GetReserveAddress(_ context.Context, exchangeID uint64) sdk.AccAddress {
	return authtypes.NewModuleAddress(ReserveModuleName(exchangeID))
}

func (k Keeper) GetReserveAddressString(ctx context.Context, exchangeID uint64) (string, error) {
	return k.accountCodec.BytesToString(k.GetReserveAddress(ctx, exchangeID))
}

func (k Keeper) ensureNextExchangeID(ctx context.Context) error {
	next, err := k.nextExchangeID.Peek(ctx)
	if err != nil {
		return err
	}
	if next == 0 {
		return k.nextExchangeID.Set(ctx, DefaultNextExchangeID)
	}
	return nil
}

func (k Keeper) nextID(ctx context.Context) (uint64, error) {
	if err := k.ensureNextExchangeID(ctx); err != nil {
		return 0, err
	}
	next, err := k.nextExchangeID.Peek(ctx)
	if err != nil {
		return 0, err
	}
	if next == ^uint64(0) {
		return 0, types.ErrInvalidRequest.Wrap("exchange id space is exhausted")
	}
	if err := k.nextExchangeID.Set(ctx, next+1); err != nil {
		return 0, err
	}
	return next, nil
}

func (k Keeper) setNextExchangeID(ctx context.Context, next uint64) error {
	if next == 0 {
		next = DefaultNextExchangeID
	}
	return k.nextExchangeID.Set(ctx, next)
}
