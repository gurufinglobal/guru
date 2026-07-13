package keeper

import (
	"context"
	"errors"
	"strings"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

func ExpectedIBCDenomForGenesis(denom, portID, channelID string) (string, error) {
	return buildIBCDenom(denom, portID, channelID)
}

func ValidateFeeDenomForGenesis(exchange *bexv1.Exchange, denom string) error {
	return validateExchangeFeeDenom(exchange, denom)
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
	if err := k.admins.Walk(ctx, nil, func(admin string) (bool, error) {
		canonical, _, err := k.canonicalAddress(admin)
		if err != nil || canonical != admin {
			return true, types.ErrInvariantViolation.Wrapf("admin address %q is not canonical", admin)
		}
		return false, nil
	}); err != nil {
		return err
	}
	totalCollected := map[string]sdkmath.Int{}
	err := k.collectedFees.Walk(ctx, nil, func(exchangeID uint64, ledger *bexv1.FeeLedger) (bool, error) {
		exchange, err := k.GetExchange(ctx, exchangeID)
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("collected fee ledger references unknown exchange %d", exchangeID)
		}
		collected, err := ledgerToCoins(ledger)
		if err != nil {
			return false, err
		}
		if err := validateExchangeFeeCoins(exchange, collected); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d collected fees contain an unsupported denom: %v", exchangeID, err)
		}
		locked, err := k.GetLockedFees(ctx, exchangeID)
		if err != nil {
			return false, err
		}
		if !hasCoins(collected, locked) {
			return false, types.ErrInvariantViolation.Wrapf("locked exceeds collected for exchange %d", exchangeID)
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED && !collected.IsZero() {
			return false, types.ErrInvariantViolation.Wrapf("deleted exchange %d has collected fees", exchangeID)
		}
		if err := accumulateFeeTotals(totalCollected, collected); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	err = k.lockedFees.Walk(ctx, nil, func(exchangeID uint64, ledger *bexv1.FeeLedger) (bool, error) {
		exchange, err := k.GetExchange(ctx, exchangeID)
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("locked fee ledger references unknown exchange %d", exchangeID)
		}
		locked, err := ledgerToCoins(ledger)
		if err != nil {
			return false, err
		}
		if err := validateExchangeFeeCoins(exchange, locked); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d locked fees contain an unsupported denom: %v", exchangeID, err)
		}
		collected, err := k.GetCollectedFees(ctx, exchangeID)
		if err != nil {
			return false, err
		}
		if !hasCoins(collected, locked) {
			return false, types.ErrInvariantViolation.Wrapf("locked exceeds collected for exchange %d", exchangeID)
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED && !locked.IsZero() {
			return false, types.ErrInvariantViolation.Wrapf("deleted exchange %d has locked fees", exchangeID)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if err := k.assertModuleBalanceCovers(ctx, feeTotalsToCoins(totalCollected)); err != nil {
		return err
	}
	maxExchangeID := uint64(0)
	err = k.exchanges.Walk(ctx, nil, func(exchangeID uint64, exchange *bexv1.Exchange) (bool, error) {
		if exchange == nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d is nil", exchangeID)
		}
		if exchange.GetId() != exchangeID {
			return false, types.ErrInvariantViolation.Wrapf("exchange map key %d does not match payload id %d", exchangeID, exchange.GetId())
		}
		if exchange.GetRevision() == 0 {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d revision must be non-zero", exchangeID)
		}
		if err := validateExchangeConfig(exchange); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d config is invalid: %v", exchangeID, err)
		}
		canonicalAdmin, _, err := k.canonicalAddress(exchange.GetAdminAddress())
		if err != nil || canonicalAdmin != exchange.GetAdminAddress() {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d admin address is not canonical", exchangeID)
		}
		for name, value := range map[string]string{
			"denom_a": exchange.GetDenomA(), "port_a": exchange.GetPortA(), "channel_a": exchange.GetChannelA(),
			"denom_b": exchange.GetDenomB(), "port_b": exchange.GetPortB(), "channel_b": exchange.GetChannelB(),
			"oracle_symbol_a_to_b": exchange.GetOracleSymbolAToB(), "oracle_symbol_b_to_a": exchange.GetOracleSymbolBToA(),
		} {
			if value != strings.TrimSpace(value) {
				return false, types.ErrInvariantViolation.Wrapf("exchange %d %s is not canonical", exchangeID, name)
			}
		}
		expectedIBCDenomA, err := buildIBCDenom(exchange.GetDenomA(), exchange.GetPortA(), exchange.GetChannelA())
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d route A is invalid: %v", exchangeID, err)
		}
		expectedIBCDenomB, err := buildIBCDenom(exchange.GetDenomB(), exchange.GetPortB(), exchange.GetChannelB())
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d route B is invalid: %v", exchangeID, err)
		}
		if exchange.GetIbcDenomA() != expectedIBCDenomA || exchange.GetIbcDenomB() != expectedIBCDenomB {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d IBC denom does not match configured route", exchangeID)
		}
		if exchangeID > maxExchangeID {
			maxExchangeID = exchangeID
		}
		expectedReserve, err := k.GetReserveAddressString(ctx, exchangeID)
		if err != nil {
			return false, err
		}
		if exchange.GetReserveAddress() != expectedReserve {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d reserve address mismatch", exchangeID)
		}
		mappedID, err := k.reserveByAddress.Get(ctx, expectedReserve)
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d reserve index missing: %v", exchangeID, err)
		}
		if mappedID != exchangeID {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d reserve index points to %d", exchangeID, mappedID)
		}
		adminIndexed, err := k.exchangesByAdmin.Has(ctx, collections.Join(exchange.GetAdminAddress(), exchangeID))
		if err != nil {
			return false, err
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
			if adminIndexed {
				return false, types.ErrInvariantViolation.Wrapf("deleted exchange %d remains in admin index", exchangeID)
			}
			if k.bankKeeper == nil {
				return false, types.ErrInvariantViolation.Wrap("bank keeper is required for reserve balance invariant")
			}
			if !k.bankKeeper.GetAllBalances(ctx, k.GetReserveAddress(ctx, exchangeID)).IsZero() {
				return false, types.ErrInvariantViolation.Wrapf("deleted exchange %d has a non-zero reserve balance", exchangeID)
			}
			collected, err := k.GetCollectedFees(ctx, exchangeID)
			if err != nil {
				return false, err
			}
			locked, err := k.GetLockedFees(ctx, exchangeID)
			if err != nil {
				return false, err
			}
			if !collected.IsZero() || !locked.IsZero() {
				return false, types.ErrInvariantViolation.Wrapf("deleted exchange %d has non-zero fee ledgers", exchangeID)
			}
		} else {
			if !adminIndexed {
				return false, types.ErrInvariantViolation.Wrapf("exchange %d missing from admin index", exchangeID)
			}
		}
		if k.accountKeeper == nil {
			return false, types.ErrInvariantViolation.Wrap("account keeper is required for reserve account invariant")
		}
		account := k.accountKeeper.GetAccount(ctx, k.GetReserveAddress(ctx, exchangeID))
		if account == nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d reserve account missing", exchangeID)
		}
		if err := validateReserveAccount(account, k.GetReserveAddress(ctx, exchangeID)); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d: %v", exchangeID, err)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	nextExchangeID, err := k.nextExchangeID.Peek(ctx)
	if err != nil {
		return err
	}
	if nextExchangeID == 0 || nextExchangeID <= maxExchangeID {
		return types.ErrInvariantViolation.Wrapf("next exchange id %d is not greater than max exchange id %d", nextExchangeID, maxExchangeID)
	}
	err = k.exchangesByAdmin.Walk(ctx, nil, func(key collections.Pair[string, uint64]) (bool, error) {
		exchange, err := k.GetExchange(ctx, key.K2())
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("admin index references unknown exchange %d", key.K2())
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
			return false, types.ErrInvariantViolation.Wrapf("admin index references deleted exchange %d", key.K2())
		}
		if exchange.GetAdminAddress() != key.K1() {
			return false, types.ErrInvariantViolation.Wrapf("admin index %s/%d does not match exchange owner", key.K1(), key.K2())
		}
		canonical, _, err := k.canonicalAddress(key.K1())
		if err != nil || canonical != key.K1() {
			return false, types.ErrInvariantViolation.Wrapf("admin index address %q is not canonical", key.K1())
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	err = k.reserveByAddress.Walk(ctx, nil, func(address string, exchangeID uint64) (bool, error) {
		exchange, err := k.GetExchange(ctx, exchangeID)
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("reserve index %s references unknown exchange %d", address, exchangeID)
		}
		if exchange.GetReserveAddress() != address {
			return false, types.ErrInvariantViolation.Wrapf("reserve index %s does not match exchange %d", address, exchangeID)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	err = k.volumeWindow.Walk(ctx, nil, func(key volumeWindowKey, amount string) (bool, error) {
		if _, err := k.GetExchange(ctx, key.K1()); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("volume window references unknown exchange %d", key.K1())
		}
		direction := bexv1.SwapDirection(key.K2())
		if direction != bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B && direction != bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A {
			return false, types.ErrInvariantViolation.Wrapf("volume window has invalid direction %d", key.K2())
		}
		if err := validateVolumeEpochSeconds("volume_window.epoch_seconds", key.K4(), false); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("volume window has invalid epoch: %v", err)
		}
		parsed, err := validateRequiredIntString("volume_window.amount", amount)
		if err != nil || parsed.IsNil() || parsed.IsNegative() {
			return false, types.ErrInvariantViolation.Wrapf("volume window has invalid amount %q", amount)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	err = k.volumePruneCursor.Walk(ctx, nil, func(key collections.Pair[uint64, uint32], _ uint64) (bool, error) {
		if _, err := k.GetExchange(ctx, key.K1()); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("volume prune cursor references unknown exchange %d", key.K1())
		}
		direction := bexv1.SwapDirection(key.K2())
		if direction != bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B && direction != bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A {
			return false, types.ErrInvariantViolation.Wrapf("volume prune cursor has invalid direction %d", key.K2())
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	err = k.reserveDepositors.Walk(ctx, nil, func(key collections.Pair[uint64, string]) (bool, error) {
		if _, err := k.GetExchange(ctx, key.K1()); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("reserve depositor references unknown exchange %d", key.K1())
		}
		canonical, _, err := k.canonicalAddress(key.K2())
		if err != nil || canonical != key.K2() {
			return false, types.ErrInvariantViolation.Wrapf("reserve depositor address %q is not canonical", key.K2())
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}
