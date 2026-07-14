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
	vestingexported "github.com/cosmos/cosmos-sdk/x/auth/vesting/exported"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

const DefaultNextExchangeID uint64 = 1

// volumeWindowKey is ordered by (expiry, exchange, direction,
// (epoch seconds, generation)), so bounded global pruning always encounters
// the earliest-expiring window first while every logical accounting
// configuration retains a distinct key.
type volumeWindowIdentity = collections.Pair[uint32, uint64]
type volumeWindowKey = collections.Quad[uint64, uint64, uint32, volumeWindowIdentity]

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
	reserveDepositors collections.KeySet[collections.Pair[uint64, string]]

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
		collections.QuadKeyCodec(
			collections.Uint64Key,
			collections.Uint64Key,
			collections.Uint32Key,
			collections.PairKeyCodec(collections.Uint32Key, collections.Uint64Key),
		),
		collections.StringValue,
	)
	k.reserveDepositors = collections.NewKeySet(
		sb,
		types.ReserveDepositorsKey,
		"reserve_depositors",
		collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
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

func executeStateTransition(ctx context.Context, fn func(sdk.Context) error) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if _, ok := ctx.(sdk.Context); !ok {
		// Preserve outer deadlines and context capabilities when a trusted module
		// wraps sdk.Context before entering the state transition.
		sdkCtx = sdkCtx.WithContext(ctx)
	}
	cacheCtx, write := sdkCtx.CacheContext()
	if err := fn(cacheCtx); err != nil {
		return err
	}
	write()
	return nil
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

func (k Keeper) ensureReserveAccount(ctx context.Context, exchangeID uint64) error {
	reserve := k.GetReserveAddress(ctx, exchangeID)
	account := k.accountKeeper.GetAccount(ctx, reserve)
	if account == nil {
		account = k.accountKeeper.NewAccountWithAddress(ctx, reserve)
		k.accountKeeper.SetAccount(ctx, account)
	}
	validationErr := validateReserveAccount(account, reserve)
	if validationErr == nil {
		return nil
	}

	// A vesting message can create an arbitrary recipient account before its
	// deterministic address becomes a registered reserve. Future deterministic
	// reserve addresses are a protocol-reserved namespace: reclaim only a
	// keyless, undelegated vesting account, intentionally retire its vesting
	// lock, and absorb its unchanged bank balance into reserve custody. Auth
	// replay metadata is preserved. This prevents permanent registration DoS.
	vestingAccount, ok := account.(vestingexported.VestingAccount)
	if !ok {
		return validationErr
	}
	if !account.GetAddress().Equals(reserve) {
		return types.ErrInvariantViolation.Wrap("reserve account address mismatch")
	}
	if account.GetPubKey() != nil {
		return types.ErrInvariantViolation.Wrap("reserve account must not have a public key")
	}
	if !vestingAccount.GetDelegatedFree().IsZero() || !vestingAccount.GetDelegatedVesting().IsZero() {
		return types.ErrInvariantViolation.Wrap("pre-existing reserve vesting account has delegated funds")
	}
	recovered := authtypes.NewBaseAccount(reserve, nil, account.GetAccountNumber(), account.GetSequence())
	k.accountKeeper.SetAccount(ctx, recovered)
	return validateReserveAccount(recovered, reserve)
}

func validateReserveAccount(account sdk.AccountI, expected sdk.AccAddress) error {
	baseAccount, ok := account.(*authtypes.BaseAccount)
	if !ok || baseAccount == nil {
		return types.ErrInvariantViolation.Wrap("reserve must be a base account")
	}
	if !baseAccount.GetAddress().Equals(expected) {
		return types.ErrInvariantViolation.Wrap("reserve account address mismatch")
	}
	if baseAccount.PubKey != nil {
		return types.ErrInvariantViolation.Wrap("reserve account must not have a public key")
	}
	return nil
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
