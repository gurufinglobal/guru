package keeper

import (
	"context"
	"errors"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

func (k Keeper) CanonicalAddressForGenesis(address string) (string, sdk.AccAddress, error) {
	return k.canonicalAddress(address)
}

func ValidateExchangeForGenesis(exchange *bexv1.Exchange) error {
	return validateExchangeConfig(exchange)
}

func ProtoCoinsForGenesis(coins []*basev1beta1.Coin) (sdk.Coins, error) {
	return ledgerToCoins(&bexv1.FeeLedger{Coins: coins})
}

func HasCoinsForGenesis(balance sdk.Coins, needed sdk.Coins) bool {
	return hasCoins(balance, needed)
}

func ParseIntForGenesis(name, value string) (sdkmath.Int, error) {
	return validateRequiredIntString(name, value)
}

func ValidateVolumeEpochForGenesis(name string, seconds uint32, allowZero bool) error {
	return validateVolumeEpochSeconds(name, seconds, allowZero)
}

func (k Keeper) ImportGenesis(ctx context.Context, genesis *bexv1.GenesisState) error {
	for _, admin := range genesis.GetAdmins() {
		canonical, _, err := k.canonicalAddress(admin)
		if err != nil {
			return err
		}
		if err := k.admins.Set(ctx, canonical); err != nil {
			return err
		}
	}
	for _, exchange := range genesis.GetExchanges() {
		copied := cloneExchange(exchange)
		if err := k.exchanges.Set(ctx, copied.GetId(), copied); err != nil {
			return err
		}
		if copied.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
			if err := k.exchangesByAdmin.Set(ctx, collections.Join(copied.GetAdminAddress(), copied.GetId())); err != nil {
				return err
			}
		}
		if err := k.reserveByAddress.Set(ctx, copied.GetReserveAddress(), copied.GetId()); err != nil {
			return err
		}
		if err := k.ensureReserveAccount(ctx, copied.GetId()); err != nil {
			return err
		}
	}
	for _, fee := range genesis.GetCollectedFees() {
		coins, err := ProtoCoinsForGenesis(fee.GetCoins())
		if err != nil {
			return err
		}
		if err := k.collectedFees.Set(ctx, fee.GetExchangeId(), coinsToLedger(coins)); err != nil {
			return err
		}
	}
	for _, fee := range genesis.GetLockedFees() {
		coins, err := ProtoCoinsForGenesis(fee.GetCoins())
		if err != nil {
			return err
		}
		if err := k.lockedFees.Set(ctx, fee.GetExchangeId(), coinsToLedger(coins)); err != nil {
			return err
		}
	}
	for _, window := range genesis.GetVolumeWindows() {
		amount, err := ParseIntForGenesis("volume amount", window.GetAmount())
		if err != nil {
			return err
		}
		key := collections.Join4(window.GetExchangeId(), uint32(window.GetDirection()), window.GetEpochStartUnix(), window.GetEpochSeconds())
		if err := k.volumeWindow.Set(ctx, key, amount.String()); err != nil {
			return err
		}
	}
	for _, depositor := range genesis.GetReserveDepositors() {
		canonical, _, err := k.canonicalAddress(depositor.GetDepositorAddress())
		if err != nil {
			return err
		}
		if err := k.reserveDepositors.Set(ctx, collections.Join(depositor.GetExchangeId(), canonical)); err != nil {
			return err
		}
	}
	return k.setNextExchangeID(ctx, genesis.GetNextExchangeId())
}

func (k Keeper) ExportGenesis(ctx context.Context) (*bexv1.GenesisState, error) {
	genesis := &bexv1.GenesisState{}
	if err := k.admins.Walk(ctx, nil, func(admin string) (bool, error) {
		genesis.Admins = append(genesis.Admins, admin)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.exchanges.Walk(ctx, nil, func(_ uint64, exchange *bexv1.Exchange) (bool, error) {
		genesis.Exchanges = append(genesis.Exchanges, cloneExchange(exchange))
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.collectedFees.Walk(ctx, nil, func(exchangeID uint64, ledger *bexv1.FeeLedger) (bool, error) {
		genesis.CollectedFees = append(genesis.CollectedFees, &bexv1.FeeGenesis{ExchangeId: exchangeID, Coins: ledger.GetCoins()})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.lockedFees.Walk(ctx, nil, func(exchangeID uint64, ledger *bexv1.FeeLedger) (bool, error) {
		genesis.LockedFees = append(genesis.LockedFees, &bexv1.FeeGenesis{ExchangeId: exchangeID, Coins: ledger.GetCoins()})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.volumeWindow.Walk(ctx, nil, func(key volumeWindowKey, amount string) (bool, error) {
		genesis.VolumeWindows = append(genesis.VolumeWindows, &bexv1.VolumeWindowGenesis{
			ExchangeId:     key.K1(),
			Direction:      bexv1.SwapDirection(key.K2()),
			EpochStartUnix: key.K3(),
			EpochSeconds:   key.K4(),
			Amount:         amount,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.reserveDepositors.Walk(ctx, nil, func(key collections.Pair[uint64, string]) (bool, error) {
		genesis.ReserveDepositors = append(genesis.ReserveDepositors, &bexv1.ReserveDepositorGenesis{
			ExchangeId:       key.K1(),
			DepositorAddress: key.K2(),
		})
		return false, nil
	}); err != nil {
		return nil, err
	}
	next, err := k.nextExchangeID.Peek(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	if next == 0 {
		next = DefaultNextExchangeID
	}
	genesis.NextExchangeId = next
	return genesis, nil
}

func (k Keeper) AssertInvariants(ctx context.Context) error {
	totalCollected := sdk.Coins{}
	err := k.collectedFees.Walk(ctx, nil, func(exchangeID uint64, ledger *bexv1.FeeLedger) (bool, error) {
		collected, err := ledgerToCoins(ledger)
		if err != nil {
			return false, err
		}
		locked, err := k.GetLockedFees(ctx, exchangeID)
		if err != nil {
			return false, err
		}
		if !hasCoins(collected, locked) {
			return false, types.ErrInvariantViolation.Wrapf("locked exceeds collected for exchange %d", exchangeID)
		}
		totalCollected = totalCollected.Add(collected...)
		return false, nil
	})
	if err != nil {
		return err
	}
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	if !hasCoins(k.bankKeeper.GetAllBalances(ctx, moduleAddr), totalCollected) {
		return types.ErrInvariantViolation.Wrap("module account balance is less than collected fees")
	}
	err = k.exchanges.Walk(ctx, nil, func(exchangeID uint64, exchange *bexv1.Exchange) (bool, error) {
		if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
			return false, nil
		}
		if k.accountKeeper == nil {
			return false, types.ErrInvariantViolation.Wrap("account keeper is required for reserve account invariant")
		}
		if k.accountKeeper.GetAccount(ctx, k.GetReserveAddress(ctx, exchangeID)) == nil {
			return false, types.ErrInvariantViolation.Wrapf("active exchange %d reserve account missing", exchangeID)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}
