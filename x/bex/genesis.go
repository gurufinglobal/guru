package bex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"cosmossdk.io/core/appmodule"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

func (am AppModule) DefaultGenesis(target appmodule.GenesisTarget) error {
	return writeGenesisState(target, am.defaultGenesisState())
}

func (am AppModule) ValidateGenesis(source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	return am.validateGenesisState(context.Background(), genesis)
}

func (am AppModule) InitGenesis(ctx context.Context, source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	if err := am.validateGenesisState(ctx, genesis); err != nil {
		return err
	}
	if err := am.keeper.ImportGenesis(ctx, genesis); err != nil {
		return err
	}
	return am.keeper.AssertInvariants(ctx)
}

func (am AppModule) ExportGenesis(ctx context.Context, target appmodule.GenesisTarget) error {
	genesis, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		return err
	}
	return writeGenesisState(target, genesis)
}

func (am AppModule) defaultGenesisState() *bexv1.GenesisState {
	return &bexv1.GenesisState{NextExchangeId: bexkeeper.DefaultNextExchangeID}
}

func (am AppModule) validateGenesisState(ctx context.Context, genesis *bexv1.GenesisState) error {
	if genesis == nil {
		return types.ErrInvalidGenesis.Wrap("genesis state cannot be nil")
	}
	seenAdmins := map[string]struct{}{}
	for _, admin := range genesis.GetAdmins() {
		canonical, _, err := am.keeper.CanonicalAddressForGenesis(admin)
		if err != nil {
			return types.ErrInvalidGenesis.Wrapf("invalid admin %q: %v", admin, err)
		}
		if _, ok := seenAdmins[canonical]; ok {
			return types.ErrInvalidGenesis.Wrapf("duplicate admin %q", canonical)
		}
		if admin != canonical {
			return types.ErrInvalidGenesis.Wrapf("admin %q is not canonical", admin)
		}
		seenAdmins[canonical] = struct{}{}
	}
	maxID := uint64(0)
	exchangeIDs := map[uint64]*bexv1.Exchange{}
	for _, exchange := range genesis.GetExchanges() {
		if exchange.GetId() == 0 {
			return types.ErrInvalidGenesis.Wrap("exchange id cannot be zero")
		}
		if _, ok := exchangeIDs[exchange.GetId()]; ok {
			return types.ErrInvalidGenesis.Wrapf("duplicate exchange id %d", exchange.GetId())
		}
		if exchange.GetId() > maxID {
			maxID = exchange.GetId()
		}
		if exchange.GetRevision() == 0 {
			return types.ErrInvalidGenesis.Wrapf("exchange %d revision must be non-zero", exchange.GetId())
		}
		if exchange.GetVolumeWindowGeneration() == 0 {
			return types.ErrInvalidGenesis.Wrapf("exchange %d volume_window_generation must be non-zero", exchange.GetId())
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
				return types.ErrInvalidGenesis.Wrapf("exchange %d %s is not canonical", exchange.GetId(), field.name)
			}
		}
		expectedReserve, err := am.keeper.GetReserveAddressString(ctx, exchange.GetId())
		if err != nil {
			return err
		}
		if exchange.GetReserveAddress() != expectedReserve {
			return types.ErrInvalidGenesis.Wrapf("exchange %d reserve address mismatch", exchange.GetId())
		}
		canonicalAdmin, _, err := am.keeper.CanonicalAddressForGenesis(exchange.GetAdminAddress())
		if err != nil {
			return types.ErrInvalidGenesis.Wrapf("invalid exchange admin: %v", err)
		}
		if exchange.GetAdminAddress() != canonicalAdmin {
			return types.ErrInvalidGenesis.Wrapf("exchange %d admin address is not canonical", exchange.GetId())
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
			if _, err := bexkeeper.ParseIntForGenesis(field.name, field.value); err != nil {
				return err
			}
		}
		expectedDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis(exchange.GetDenomA(), exchange.GetPortA(), exchange.GetChannelA())
		if err != nil {
			return err
		}
		expectedDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis(exchange.GetDenomB(), exchange.GetPortB(), exchange.GetChannelB())
		if err != nil {
			return err
		}
		if exchange.GetIbcDenomA() != expectedDenomA || exchange.GetIbcDenomB() != expectedDenomB {
			return types.ErrInvalidGenesis.Wrapf("exchange %d IBC denom does not match configured route", exchange.GetId())
		}
		if err := bexkeeper.ValidateExchangeForGenesis(exchange); err != nil {
			return err
		}
		exchangeIDs[exchange.GetId()] = exchange
	}
	if genesis.GetNextExchangeId() <= maxID {
		return types.ErrInvalidGenesis.Wrap("next_exchange_id must be greater than max exchange id")
	}
	collectedByID := map[uint64]sdk.Coins{}
	totalCollected := map[string]sdkmath.Int{}
	for _, fee := range genesis.GetCollectedFees() {
		exchange, ok := exchangeIDs[fee.GetExchangeId()]
		if !ok {
			return types.ErrInvalidGenesis.Wrapf("collected fees reference unknown exchange %d", fee.GetExchangeId())
		}
		if _, exists := collectedByID[fee.GetExchangeId()]; exists {
			return types.ErrInvalidGenesis.Wrapf("duplicate collected fee ledger for exchange %d", fee.GetExchangeId())
		}
		coins, err := bexkeeper.ProtoCoinsForGenesis(fee.GetCoins())
		if err != nil {
			return err
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED && !coins.IsZero() {
			return types.ErrInvalidGenesis.Wrapf("deleted exchange %d has collected fees", fee.GetExchangeId())
		}
		for _, coin := range coins {
			if err := bexkeeper.ValidateFeeDenomForGenesis(exchange, coin.Denom); err != nil {
				return types.ErrInvalidGenesis.Wrapf("exchange %d collected fees: %v", fee.GetExchangeId(), err)
			}
		}
		collectedByID[fee.GetExchangeId()] = coins
		for _, coin := range coins {
			current, ok := totalCollected[coin.Denom]
			if !ok {
				current = sdkmath.ZeroInt()
			}
			next, err := current.SafeAdd(coin.Amount)
			if err != nil {
				return types.ErrInvalidGenesis.Wrapf("total collected fees for %s exceed uint256 max", coin.Denom)
			}
			totalCollected[coin.Denom] = next
		}
	}
	lockedIDs := map[uint64]struct{}{}
	for _, fee := range genesis.GetLockedFees() {
		exchange, ok := exchangeIDs[fee.GetExchangeId()]
		if !ok {
			return types.ErrInvalidGenesis.Wrapf("locked fees reference unknown exchange %d", fee.GetExchangeId())
		}
		if _, exists := lockedIDs[fee.GetExchangeId()]; exists {
			return types.ErrInvalidGenesis.Wrapf("duplicate locked fee ledger for exchange %d", fee.GetExchangeId())
		}
		lockedIDs[fee.GetExchangeId()] = struct{}{}
		locked, err := bexkeeper.ProtoCoinsForGenesis(fee.GetCoins())
		if err != nil {
			return err
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED && !locked.IsZero() {
			return types.ErrInvalidGenesis.Wrapf("deleted exchange %d has locked fees", fee.GetExchangeId())
		}
		for _, coin := range locked {
			if err := bexkeeper.ValidateFeeDenomForGenesis(exchange, coin.Denom); err != nil {
				return types.ErrInvalidGenesis.Wrapf("exchange %d locked fees: %v", fee.GetExchangeId(), err)
			}
		}
		if !bexkeeper.HasCoinsForGenesis(collectedByID[fee.GetExchangeId()], locked) {
			return types.ErrInvalidGenesis.Wrapf("locked fees exceed collected fees for exchange %d", fee.GetExchangeId())
		}
	}
	pendingIDs := map[uint64]struct{}{}
	for _, liability := range genesis.GetPendingLiabilities() {
		exchange, ok := exchangeIDs[liability.GetExchangeId()]
		if !ok {
			return types.ErrInvalidGenesis.Wrapf("pending liabilities reference unknown exchange %d", liability.GetExchangeId())
		}
		if _, exists := pendingIDs[liability.GetExchangeId()]; exists {
			return types.ErrInvalidGenesis.Wrapf("duplicate pending liability ledger for exchange %d", liability.GetExchangeId())
		}
		pendingIDs[liability.GetExchangeId()] = struct{}{}
		pending, err := bexkeeper.ProtoCoinsForGenesis(liability.GetCoins())
		if err != nil {
			return err
		}
		if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED && !pending.IsZero() {
			return types.ErrInvalidGenesis.Wrapf("deleted exchange %d has pending liabilities", liability.GetExchangeId())
		}
		for _, coin := range pending {
			if err := bexkeeper.ValidateFeeDenomForGenesis(exchange, coin.Denom); err != nil {
				return types.ErrInvalidGenesis.Wrapf("exchange %d pending liabilities: %v", liability.GetExchangeId(), err)
			}
		}
	}
	volumeKeys := map[string]struct{}{}
	for _, window := range genesis.GetVolumeWindows() {
		exchange, ok := exchangeIDs[window.GetExchangeId()]
		if !ok {
			return types.ErrInvalidGenesis.Wrapf("volume window references unknown exchange %d", window.GetExchangeId())
		}
		if window.GetDirection() != bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B &&
			window.GetDirection() != bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A {
			return types.ErrInvalidGenesis.Wrap("invalid volume window")
		}
		key := fmt.Sprintf(
			"%d/%d/%d/%d/%d",
			window.GetExchangeId(),
			window.GetDirection(),
			window.GetEpochStartUnix(),
			window.GetEpochSeconds(),
			window.GetVolumeWindowGeneration(),
		)
		if _, exists := volumeKeys[key]; exists {
			return types.ErrInvalidGenesis.Wrapf("duplicate volume window %s", key)
		}
		volumeKeys[key] = struct{}{}
		if err := bexkeeper.ValidateVolumeWindowForGenesis(
			window.GetEpochStartUnix(),
			window.GetEpochSeconds(),
			window.GetVolumeWindowGeneration(),
		); err != nil {
			return err
		}
		if window.GetVolumeWindowGeneration() > exchange.GetVolumeWindowGeneration() {
			return types.ErrInvalidGenesis.Wrapf(
				"volume window generation %d exceeds exchange %d generation %d",
				window.GetVolumeWindowGeneration(),
				window.GetExchangeId(),
				exchange.GetVolumeWindowGeneration(),
			)
		}
		if _, err := bexkeeper.ParseIntForGenesis("volume amount", window.GetAmount()); err != nil {
			return err
		}
	}
	depositorKeys := map[string]struct{}{}
	for _, depositor := range genesis.GetReserveDepositors() {
		if _, ok := exchangeIDs[depositor.GetExchangeId()]; !ok {
			return types.ErrInvalidGenesis.Wrapf("reserve depositor references unknown exchange %d", depositor.GetExchangeId())
		}
		canonical, _, err := am.keeper.CanonicalAddressForGenesis(depositor.GetDepositorAddress())
		if err != nil {
			return types.ErrInvalidGenesis.Wrapf("invalid reserve depositor: %v", err)
		}
		if depositor.GetDepositorAddress() != canonical {
			return types.ErrInvalidGenesis.Wrapf("reserve depositor %q is not canonical", depositor.GetDepositorAddress())
		}
		key := fmt.Sprintf("%d/%s", depositor.GetExchangeId(), canonical)
		if _, exists := depositorKeys[key]; exists {
			return types.ErrInvalidGenesis.Wrapf("duplicate reserve depositor %s", key)
		}
		depositorKeys[key] = struct{}{}
	}
	return nil
}

func readGenesisState(source appmodule.GenesisSource, defaults *bexv1.GenesisState) (*bexv1.GenesisState, error) {
	genesis := &bexv1.GenesisState{NextExchangeId: defaults.GetNextExchangeId()}

	admins := []string{}
	found, err := readGenesisField(source, "admins", &admins)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.Admins = admins
	}

	exchanges := []*bexv1.Exchange{}
	found, err = readGenesisField(source, "exchanges", &exchanges)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.Exchanges = exchanges
	}

	collectedFees := []*bexv1.FeeGenesis{}
	found, err = readGenesisField(source, "collected_fees", &collectedFees)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.CollectedFees = collectedFees
	}

	lockedFees := []*bexv1.FeeGenesis{}
	found, err = readGenesisField(source, "locked_fees", &lockedFees)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.LockedFees = lockedFees
	}

	pendingLiabilities := []*bexv1.FeeGenesis{}
	found, err = readGenesisField(source, "pending_liabilities", &pendingLiabilities)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.PendingLiabilities = pendingLiabilities
	}

	volumeWindows := []*bexv1.VolumeWindowGenesis{}
	found, err = readGenesisField(source, "volume_windows", &volumeWindows)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.VolumeWindows = volumeWindows
	}

	reserveDepositors := []*bexv1.ReserveDepositorGenesis{}
	found, err = readGenesisField(source, "reserve_depositors", &reserveDepositors)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.ReserveDepositors = reserveDepositors
	}

	nextExchangeID := genesis.GetNextExchangeId()
	found, err = readGenesisField(source, "next_exchange_id", &nextExchangeID)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.NextExchangeId = nextExchangeID
	}

	return genesis, nil
}

func writeGenesisState(target appmodule.GenesisTarget, genesis *bexv1.GenesisState) error {
	if genesis == nil {
		return types.ErrInvalidGenesis.Wrap("genesis state cannot be nil")
	}

	if err := writeGenesisField(target, "admins", genesis.GetAdmins()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "exchanges", genesis.GetExchanges()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "collected_fees", genesis.GetCollectedFees()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "locked_fees", genesis.GetLockedFees()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "pending_liabilities", genesis.GetPendingLiabilities()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "volume_windows", genesis.GetVolumeWindows()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "reserve_depositors", genesis.GetReserveDepositors()); err != nil {
		return err
	}

	return writeGenesisField(target, "next_exchange_id", genesis.GetNextExchangeId())
}

func readGenesisField(source appmodule.GenesisSource, fieldName string, value any) (bool, error) {
	reader, err := source(fieldName)
	if err != nil {
		return false, types.ErrReadGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	if reader == nil {
		return false, nil
	}

	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		_ = reader.Close()
		return false, types.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		_ = reader.Close()
		if err == nil {
			return false, types.ErrDecodeGenesisField.Wrapf("%s: unexpected trailing JSON value", fieldName)
		}
		return false, types.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	if err := reader.Close(); err != nil {
		return false, types.ErrReadGenesisField.Wrapf("%s: close reader: %v", fieldName, err)
	}

	return true, nil
}

func writeGenesisField(target appmodule.GenesisTarget, fieldName string, value any) error {
	writer, err := target(fieldName)
	if err != nil {
		return types.ErrOpenGenesisTargetField.Wrapf("%s: %v", fieldName, err)
	}
	if writer == nil {
		return types.ErrNilGenesisTargetWriter.Wrapf("%s genesis target field writer is nil", fieldName)
	}

	if err := json.NewEncoder(writer).Encode(value); err != nil {
		_ = writer.Close()
		return types.ErrEncodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}

	if err := writer.Close(); err != nil {
		return types.ErrCloseGenesisFieldWriter.Wrapf("%s: %v", fieldName, err)
	}

	return nil
}
