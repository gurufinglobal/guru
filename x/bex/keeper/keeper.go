package keeper

import (
	"fmt"

	"cosmossdk.io/log"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"
	"github.com/GPTx-global/guru-v2/x/bex/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
	authority string
}

func NewKeeper(
	cdc codec.BinaryCodec,
	key storetypes.StoreKey,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	authority string,
) Keeper {

	// ensure bex module account is set
	if addr := ak.GetModuleAddress(types.ModuleName); addr == nil {
		panic("the bex module account has not been set")
	}

	return Keeper{
		cdc:        cdc,
		storeKey:   key,
		bankKeeper: bk,
		authority:  authority,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetAuthority() string {
	return k.authority
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
func (k Keeper) SetModeratorAddress(ctx sdk.Context, moderator_address string) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.KeyModeratorAddress, []byte(moderator_address))
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
func (k Keeper) WithdrawExchangeFees(ctx sdk.Context, exchangeId string, withdrawAddress string) error {
	store := ctx.KVStore(k.storeKey)
	exchangeRequestsStore := prefix.NewStore(store, types.KeyExchanges)
	bz := exchangeRequestsStore.Get([]byte(exchangeId))
	if bz == nil {
		return fmt.Errorf("exchange not found")
	}
	var exchange types.Exchange
	k.cdc.MustUnmarshal(bz, &exchange)

	accAddrSender, err := sdk.AccAddressFromBech32(exchange.ReserveAddress)
	if err != nil {
		return fmt.Errorf("invalid sender address")
	}

	accAddressReceiver, err := sdk.AccAddressFromBech32(withdrawAddress)
	if err != nil {
		return fmt.Errorf("invalid receiver address")
	}

	amount := exchange.AccumulatedFee

	if amount.IsZero() {
		return fmt.Errorf("no fees to withdraw")
	}

	return k.bankKeeper.SendCoins(ctx, accAddrSender, accAddressReceiver, amount)

}
