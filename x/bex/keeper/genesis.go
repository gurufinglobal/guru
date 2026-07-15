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
	if err := validateExchangeConfig(exchange); err != nil {
		return types.ErrInvalidGenesis.Wrapf("invalid exchange: %v", err)
	}
	return nil
}

func ProtoCoinsForGenesis(coins []*basev1beta1.Coin) (sdk.Coins, error) {
	parsed, err := ledgerToCoins(&bexv1.FeeLedger{Coins: coins})
	if err != nil {
		return nil, types.ErrInvalidGenesis.Wrapf("invalid coin list: %v", err)
	}
	canonical := sdkCoinsToProto(parsed)
	if len(coins) != len(canonical) {
		return nil, types.ErrInvalidGenesis.Wrap("coin list must be canonical and sorted")
	}
	for i, coin := range coins {
		if coin == nil || coin.GetDenom() != canonical[i].GetDenom() || coin.GetAmount() != canonical[i].GetAmount() {
			return nil, types.ErrInvalidGenesis.Wrap("coin list must be canonical and sorted")
		}
	}
	return parsed, nil
}

func HasCoinsForGenesis(balance sdk.Coins, needed sdk.Coins) bool {
	return hasCoins(balance, needed)
}

func ParseIntForGenesis(name, value string) (sdkmath.Int, error) {
	amount, err := validateRequiredIntString(name, value)
	if err != nil {
		return sdkmath.Int{}, types.ErrInvalidGenesis.Wrapf("invalid %s: %v", name, err)
	}
	if value != amount.String() {
		return sdkmath.Int{}, types.ErrInvalidGenesis.Wrapf("%s is not a canonical decimal", name)
	}
	return amount, nil
}

func ValidateVolumeEpochForGenesis(name string, seconds uint32, allowZero bool) error {
	if err := validateVolumeEpochSeconds(name, seconds, allowZero); err != nil {
		return types.ErrInvalidGenesis.Wrapf("invalid %s: %v", name, err)
	}
	return nil
}

func ValidateVolumeWindowForGenesis(epochStart uint64, epochSeconds uint32, generation uint64) error {
	if err := validateVolumeEpochSeconds("volume_window.epoch_seconds", epochSeconds, false); err != nil {
		return types.ErrInvalidGenesis.Wrapf("invalid volume window epoch: %v", err)
	}
	if generation == 0 {
		return types.ErrInvalidGenesis.Wrap("volume_window.volume_window_generation must be non-zero")
	}
	if epochStart%uint64(epochSeconds) != 0 {
		return types.ErrInvalidGenesis.Wrap("volume_window.epoch_start_unix must align to epoch_seconds")
	}
	if epochStart > ^uint64(0)-uint64(epochSeconds) {
		return types.ErrInvalidGenesis.Wrap("volume_window expiry overflows uint64")
	}
	return nil
}

func ExpectedIBCDenomForGenesis(denom, portID, channelID string) (string, error) {
	ibcDenom, err := buildIBCDenom(denom, portID, channelID)
	if err != nil {
		return "", types.ErrInvalidGenesis.Wrapf("invalid IBC route: %v", err)
	}
	return ibcDenom, nil
}

func ValidateFeeDenomForGenesis(exchange *bexv1.Exchange, denom string) error {
	if err := validateExchangeFeeDenom(exchange, denom); err != nil {
		return types.ErrInvalidGenesis.Wrapf("invalid fee denom: %v", err)
	}
	return nil
}

func ValidateReserveDenomForGenesis(exchange *bexv1.Exchange, denom string) error {
	if err := validateExchangeReserveDenom(exchange, denom); err != nil {
		return types.ErrInvalidGenesis.Wrapf("invalid reserve denom: %v", err)
	}
	return nil
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
	for _, liability := range genesis.GetPendingLiabilities() {
		coins, err := ProtoCoinsForGenesis(liability.GetCoins())
		if err != nil {
			return err
		}
		if err := k.pendingLiabilities.Set(ctx, liability.GetExchangeId(), coinsToLedger(coins)); err != nil {
			return err
		}
	}
	for _, window := range genesis.GetVolumeWindows() {
		if err := ValidateVolumeWindowForGenesis(window.GetEpochStartUnix(), window.GetEpochSeconds(), window.GetVolumeWindowGeneration()); err != nil {
			return err
		}
		amount, err := ParseIntForGenesis("volume amount", window.GetAmount())
		if err != nil {
			return err
		}
		key := volumeWindowKeyFromStart(
			window.GetExchangeId(),
			window.GetDirection(),
			window.GetEpochStartUnix(),
			window.GetEpochSeconds(),
			window.GetVolumeWindowGeneration(),
		)
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
	if err := k.AssertInvariants(ctx); err != nil {
		return nil, err
	}

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
	if err := k.pendingLiabilities.Walk(ctx, nil, func(exchangeID uint64, ledger *bexv1.FeeLedger) (bool, error) {
		genesis.PendingLiabilities = append(genesis.PendingLiabilities, &bexv1.FeeGenesis{ExchangeId: exchangeID, Coins: ledger.GetCoins()})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.volumeWindow.Walk(ctx, nil, func(key volumeWindowKey, amount string) (bool, error) {
		epochStart, ok := volumeWindowEpochStart(key)
		if !ok {
			return true, types.ErrInvariantViolation.Wrap("volume window key has an invalid expiry")
		}
		genesis.VolumeWindows = append(genesis.VolumeWindows, &bexv1.VolumeWindowGenesis{
			ExchangeId:             key.K2(),
			Direction:              bexv1.SwapDirection(key.K3()),
			EpochStartUnix:         epochStart,
			EpochSeconds:           volumeWindowEpochSeconds(key),
			Amount:                 amount,
			VolumeWindowGeneration: volumeWindowGeneration(key),
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
			return false, types.ErrInvariantViolation.Wrapf("exchange %d collected fee ledger is invalid: %v", exchangeID, err)
		}
		if err := assertCanonicalFeeLedger("collected", exchangeID, ledger, collected); err != nil {
			return false, err
		}
		if err := validateExchangeFeeCoins(exchange, collected); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d collected fees contain an unsupported denom: %v", exchangeID, err)
		}
		locked, err := k.GetLockedFees(ctx, exchangeID)
		if err != nil {
			return false, feeLedgerInvariantError("locked", exchangeID, err)
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
			return false, types.ErrInvariantViolation.Wrapf("exchange %d locked fee ledger is invalid: %v", exchangeID, err)
		}
		if err := assertCanonicalFeeLedger("locked", exchangeID, ledger, locked); err != nil {
			return false, err
		}
		if err := validateExchangeFeeCoins(exchange, locked); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d locked fees contain an unsupported denom: %v", exchangeID, err)
		}
		collected, err := k.GetCollectedFees(ctx, exchangeID)
		if err != nil {
			return false, feeLedgerInvariantError("collected", exchangeID, err)
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
	err = k.pendingLiabilities.Walk(ctx, nil, func(exchangeID uint64, ledger *bexv1.FeeLedger) (bool, error) {
		exchange, err := k.GetExchange(ctx, exchangeID)
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("pending liability references unknown exchange %d", exchangeID)
		}
		pending, err := ledgerToCoins(ledger)
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d pending liability ledger is invalid: %v", exchangeID, err)
		}
		if err := assertCanonicalFeeLedger("pending liability", exchangeID, ledger, pending); err != nil {
			return false, err
		}
		for _, coin := range pending {
			if err := validateExchangeReserveDenom(exchange, coin.Denom); err != nil {
				return false, types.ErrInvariantViolation.Wrapf("exchange %d pending liabilities contain an unsupported denom: %v", exchangeID, err)
			}
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED && !pending.IsZero() {
			return false, types.ErrInvariantViolation.Wrapf("deleted exchange %d has pending liabilities", exchangeID)
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
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "limit_a_to_b", value: exchange.GetLimitAToB()},
			{name: "limit_b_to_a", value: exchange.GetLimitBToA()},
			{name: "volume_cap_a_to_b", value: exchange.GetVolumeCapAToB()},
			{name: "volume_cap_b_to_a", value: exchange.GetVolumeCapBToA()},
		} {
			amount, err := validateExchangeLimitIntString(field.name, field.value)
			if err != nil || field.value != amount.String() {
				return false, types.ErrInvariantViolation.Wrapf("exchange %d %s is not canonical", exchangeID, field.name)
			}
		}
		canonicalAdmin, _, err := k.canonicalAddress(exchange.GetAdminAddress())
		if err != nil || canonicalAdmin != exchange.GetAdminAddress() {
			return false, types.ErrInvariantViolation.Wrapf("exchange %d admin address is not canonical", exchangeID)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "denom_a", value: exchange.GetDenomA()},
			{name: "port_a", value: exchange.GetPortA()},
			{name: "channel_a", value: exchange.GetChannelA()},
			{name: "denom_b", value: exchange.GetDenomB()},
			{name: "port_b", value: exchange.GetPortB()},
			{name: "channel_b", value: exchange.GetChannelB()},
			{name: "oracle_symbol_a_to_b", value: exchange.GetOracleSymbolAToB()},
			{name: "oracle_symbol_b_to_a", value: exchange.GetOracleSymbolBToA()},
		} {
			if field.value != strings.TrimSpace(field.value) {
				return false, types.ErrInvariantViolation.Wrapf("exchange %d %s is not canonical", exchangeID, field.name)
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
				return false, feeLedgerInvariantError("collected", exchangeID, err)
			}
			locked, err := k.GetLockedFees(ctx, exchangeID)
			if err != nil {
				return false, feeLedgerInvariantError("locked", exchangeID, err)
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
		exchangeID := key.K2()
		exchange, err := k.GetExchange(ctx, exchangeID)
		if err != nil {
			return false, types.ErrInvariantViolation.Wrapf("volume window references unknown exchange %d", exchangeID)
		}
		direction := bexv1.SwapDirection(key.K3())
		if direction != bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B && direction != bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A {
			return false, types.ErrInvariantViolation.Wrapf("volume window has invalid direction %d", key.K3())
		}
		epochStart, ok := volumeWindowEpochStart(key)
		if !ok {
			return false, types.ErrInvariantViolation.Wrap("volume window key has an invalid expiry")
		}
		generation := volumeWindowGeneration(key)
		if err := ValidateVolumeWindowForGenesis(epochStart, volumeWindowEpochSeconds(key), generation); err != nil {
			return false, types.ErrInvariantViolation.Wrapf("volume window has invalid epoch: %v", err)
		}
		if generation > exchange.GetVolumeWindowGeneration() {
			return false, types.ErrInvariantViolation.Wrapf(
				"volume window generation %d exceeds exchange %d generation %d",
				generation,
				exchangeID,
				exchange.GetVolumeWindowGeneration(),
			)
		}
		parsed, err := validateRequiredIntString("volume_window.amount", amount)
		if err != nil || parsed.IsNil() || parsed.IsNegative() || amount != parsed.String() {
			return false, types.ErrInvariantViolation.Wrapf("volume window has invalid amount %q", amount)
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

func assertCanonicalFeeLedger(name string, exchangeID uint64, ledger *bexv1.FeeLedger, canonical sdk.Coins) error {
	if ledger == nil {
		return types.ErrInvariantViolation.Wrapf("exchange %d %s fee ledger is nil", exchangeID, name)
	}
	expected := sdkCoinsToProto(canonical)
	if len(ledger.GetCoins()) != len(expected) {
		return types.ErrInvariantViolation.Wrapf("exchange %d %s fee ledger is not canonical and sorted", exchangeID, name)
	}
	for i, coin := range ledger.GetCoins() {
		if coin == nil || coin.GetDenom() != expected[i].GetDenom() || coin.GetAmount() != expected[i].GetAmount() {
			return types.ErrInvariantViolation.Wrapf(
				"exchange %d %s fee ledger is not canonical and sorted",
				exchangeID,
				name,
			)
		}
	}
	return nil
}

func feeLedgerInvariantError(name string, exchangeID uint64, err error) error {
	if errors.Is(err, types.ErrInvalidRequest) {
		return types.ErrInvariantViolation.Wrapf("exchange %d %s fee ledger is invalid: %v", exchangeID, name, err)
	}
	return err
}
