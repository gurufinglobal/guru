package keeper

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/log"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/bex/types"
)

// Keeper of the xmsquare store
type Keeper struct {
	// Protobuf codec
	cdc codec.BinaryCodec

	// Store key required for the EVM Prefix KVStore. It is required by:
	// - storing account's Storage State
	// - storing account's Code
	// - storing transaction Logs
	// - storing Bloom filters by block height. Needed for the Web3 API.
	storeKey storetypes.StoreKey

	// mint and burn coins using bankkeper
	bankKeeper types.BankKeeper

	// authority defines the default moderatpr address
	authority     string
	moduleAddress sdk.AccAddress
}

func NewKeeper(
	cdc codec.BinaryCodec,
	key storetypes.StoreKey,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	authority string,
) Keeper {
	// ensure bex module account is set
	addr := ak.GetModuleAddress(types.ModuleName)
	if addr == nil {
		panic("the bex module account has not been set")
	}

	return Keeper{
		cdc:           cdc,
		storeKey:      key,
		bankKeeper:    bk,
		authority:     authority,
		moduleAddress: addr,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetAuthority() string {
	return k.authority
}

func (k Keeper) GetModuleAddress() sdk.AccAddress {
	return k.moduleAddress
}

// GetModeratorAddress returns the current moderator address.
func (k Keeper) GetModeratorAddress(ctx sdk.Context) string {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyModeratorAddress)
	if bz == nil {
		return ""
	}
	return string(bz)
}

// SetModeratorAddress adds/updates the moderator address.
func (k Keeper) SetModeratorAddress(ctx sdk.Context, moderatorAddress string) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.KeyModeratorAddress, []byte(moderatorAddress))
}

// GetRatemeter returns the current ratemeter.
func (k Keeper) GetRatemeter(ctx sdk.Context) (*types.Ratemeter, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyRatemeter)
	if bz == nil {
		return nil, fmt.Errorf("ratemeter not found")
	}

	var ratemeter types.Ratemeter
	k.cdc.MustUnmarshal(bz, &ratemeter)

	return &ratemeter, nil
}

// SetRatemeter adds/updates the ratemeter.
func (k Keeper) SetRatemeter(ctx sdk.Context, ratemeter *types.Ratemeter) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.KeyRatemeter, k.cdc.MustMarshal(ratemeter))
}

func (k Keeper) GetAddressRequestCount(ctx sdk.Context, address string) uint64 {
	store := ctx.KVStore(k.storeKey)
	addressRequestsStore := prefix.NewStore(store, types.KeyAddressRateRegistry)

	rateRegistryBytes := addressRequestsStore.Get([]byte(address))
	if rateRegistryBytes == nil {
		return 0
	}

	var rateRegistry types.RateRegistry
	k.cdc.MustUnmarshal(rateRegistryBytes, &rateRegistry)
	ratemeter, err := k.GetRatemeter(ctx)
	if err != nil {
		return 0
	}

	now := ctx.BlockTime()
	alignedStart := now.Truncate(ratemeter.RequestPeriod)

	if rateRegistry.StartWindow < alignedStart.UnixNano() {
		addressRequestsStore.Delete([]byte(address))
		return 0
	}

	return rateRegistry.RequestCount
}

func (k Keeper) CheckAddressRateLimit(ctx sdk.Context, address string) bool {
	ratemeter, err := k.GetRatemeter(ctx)
	if err != nil {
		return false
	}
	count := k.GetAddressRequestCount(ctx, address)

	return count < ratemeter.RequestCountLimit
}

func (k Keeper) IncrementAddressRequestCount(ctx sdk.Context, address string) error {
	store := ctx.KVStore(k.storeKey)
	addressRequestsStore := prefix.NewStore(store, types.KeyAddressRateRegistry)

	ratemeter, err := k.GetRatemeter(ctx)
	if err != nil {
		return err
	}
	count := k.GetAddressRequestCount(ctx, address)
	if count >= ratemeter.RequestCountLimit {
		return fmt.Errorf("request count limit reached")
	}
	count++

	now := ctx.BlockTime()
	alignedStart := now.Truncate(ratemeter.RequestPeriod)

	rateRegistry := types.RateRegistry{
		RequestCount: count,
		StartWindow:  alignedStart.UnixNano(),
	}

	addressRequestsStore.Set([]byte(address), k.cdc.MustMarshal(&rateRegistry))
	return nil
}

// WithdrawExchangeFees withdraws the accumulated fees from the reserve address to the given address
func (k Keeper) WithdrawExchangeFees(ctx sdk.Context, exchangeID string, withdrawAddress string) error {
	fees, err := k.GetExchangeFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if fees == nil {
		return fmt.Errorf("no fees to withdraw")
	}

	accAddressReceiver, err := sdk.AccAddressFromBech32(withdrawAddress)
	if err != nil {
		return fmt.Errorf("invalid receiver address")
	}

	err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, accAddressReceiver, fees)
	if err != nil {
		return err
	}

	return nil
}

// GetTotalCollectedFees returns the total collected fees for the module
func (k Keeper) GetTotalCollectedFees(ctx sdk.Context) sdk.Coins {
	return k.bankKeeper.GetAllBalances(ctx, k.GetModuleAddress())
}

// setCollectedFees sets the collected fees for the given exchange id
func (k Keeper) setCollectedFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	store := ctx.KVStore(k.storeKey)
	exchangeIDStore := prefix.NewStore(store, types.KeyCollectedFees)
	bz, err := json.Marshal(fees)
	if err != nil {
		return err
	}
	exchangeIDStore.Set([]byte(exchangeID), bz)
	return nil
}

// GetExchangeFees returns the collected fees for the given exchange id
func (k Keeper) GetExchangeFees(ctx sdk.Context, exchangeID string) (sdk.Coins, error) {
	store := ctx.KVStore(k.storeKey)
	exchangeIDStore := prefix.NewStore(store, types.KeyCollectedFees)
	bz := exchangeIDStore.Get([]byte(exchangeID))
	if bz == nil {
		return nil, nil
	}
	fees := sdk.Coins{}
	err := json.Unmarshal(bz, &fees)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal collected fees for exchange: %s", exchangeID)
	}

	// subtract the locked fees from the collected fees to get the available fees
	lockedFees, err := k.getLockedFees(ctx, exchangeID)
	if err != nil {
		return nil, err
	}
	fees = fees.Sub(lockedFees...)

	return fees, nil
}

// AddExchangeFees adds the given fees to the collected fees for the given exchange id
func (k Keeper) AddExchangeFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	collectedFees, err := k.GetExchangeFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if collectedFees == nil {
		collectedFees = sdk.Coins{}
	}
	collectedFees = collectedFees.Add(fees...)
	return k.setCollectedFees(ctx, exchangeID, collectedFees)
}

// DeductExchangeFees deducts the given fees from the collected fees for the given exchange id
func (k Keeper) DeductExchangeFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	collectedFees, err := k.GetExchangeFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	collectedFees = collectedFees.Sub(fees...)
	return k.setCollectedFees(ctx, exchangeID, collectedFees)
}

// DeleteExchangeFees deletes the collected fees for the given exchange id
func (k Keeper) DeleteExchangeFees(ctx sdk.Context, exchangeID string) {
	store := ctx.KVStore(k.storeKey)
	exchangeIDStore := prefix.NewStore(store, types.KeyCollectedFees)
	exchangeIDStore.Delete([]byte(exchangeID))
}

// GetLockedFees returns the locked fees for the given exchange id
func (k Keeper) getLockedFees(ctx sdk.Context, exchangeID string) (sdk.Coins, error) {
	store := ctx.KVStore(k.storeKey)
	lockedFeesStore := prefix.NewStore(store, types.KeyLockedFees)
	bz := lockedFeesStore.Get([]byte(exchangeID))
	if bz == nil {
		return sdk.Coins{}, nil
	}
	lockedFees := sdk.Coins{}
	err := json.Unmarshal(bz, &lockedFees)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal locked fees for exchange: %s", exchangeID)
	}
	return lockedFees, nil
}

// LockExchangeFees locks the given fees for the given exchange id
func (k Keeper) LockExchangeFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	store := ctx.KVStore(k.storeKey)
	lockedFeesStore := prefix.NewStore(store, types.KeyLockedFees)
	lockedFees, err := k.getLockedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	lockedFees = lockedFees.Add(fees...)
	bz, err := json.Marshal(lockedFees)
	if err != nil {
		return err
	}
	lockedFeesStore.Set([]byte(exchangeID), bz)
	return nil
}

// ReleaseExchangeFees releases the given fees from the collected fees for the given exchange id
func (k Keeper) ReleaseExchangeFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	store := ctx.KVStore(k.storeKey)
	lockedFeesStore := prefix.NewStore(store, types.KeyLockedFees)
	lockedFees, err := k.getLockedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if !lockedFees.IsAllGTE(fees) {
		return fmt.Errorf("fees to release are less than the locked fees for exchange: %s", exchangeID)
	}
	lockedFees = lockedFees.Sub(fees...)
	bz, err := json.Marshal(lockedFees)
	if err != nil {
		return err
	}
	lockedFeesStore.Set([]byte(exchangeID), bz)
	return nil
}
