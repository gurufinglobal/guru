package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

// SetLockedFee stores the per-packet locked fee record.
// Keyed by (portID, channelID, sequence) of the outgoing packet.
func (k Keeper) SetLockedFee(ctx sdk.Context, portID, channelID string, sequence uint64, fee sdk.Coin) error {
	if fee.IsNil() {
		return fmt.Errorf("locked fee cannot be nil")
	}
	if !fee.Amount.IsPositive() {
		return fmt.Errorf("locked fee amount must be positive: %s", fee)
	}

	store := k.storeService.OpenKVStore(ctx)
	if err := store.Set(types.LockedFeeKey(portID, channelID, sequence), []byte(fee.String())); err != nil {
		return err
	}
	return nil
}

// GetLockedFee returns the locked fee for (portID, channelID, sequence).
func (k Keeper) GetLockedFee(ctx sdk.Context, portID, channelID string, sequence uint64) (sdk.Coin, bool, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.LockedFeeKey(portID, channelID, sequence))
	if err != nil {
		return sdk.Coin{}, false, err
	}
	if bz == nil || len(bz) == 0 {
		return sdk.Coin{}, false, nil
	}

	fee, err := sdk.ParseCoinNormalized(string(bz))
	if err != nil {
		return sdk.Coin{}, false, fmt.Errorf("failed to parse locked fee coin %q: %w", string(bz), err)
	}

	return fee, true, nil
}

// DeleteLockedFee deletes the locked fee record for (portID, channelID, sequence).
func (k Keeper) DeleteLockedFee(ctx sdk.Context, portID, channelID string, sequence uint64) {
	store := k.storeService.OpenKVStore(ctx)
	_ = store.Delete(types.LockedFeeKey(portID, channelID, sequence))
}

