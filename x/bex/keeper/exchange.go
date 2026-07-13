package keeper

import (
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"google.golang.org/protobuf/proto"
)

func (k Keeper) RegisterAdmin(ctx context.Context, moderator, admin string) error {
	if err := k.validateModerator(ctx, moderator); err != nil {
		return err
	}
	canonical, _, err := k.canonicalAddress(admin)
	if err != nil {
		return types.ErrInvalidRequest.Wrapf("invalid admin address: %v", err)
	}
	has, err := k.admins.Has(ctx, canonical)
	if err != nil {
		return err
	}
	if has {
		return types.ErrInvalidRequest.Wrap("admin already registered")
	}
	if err := k.admins.Set(ctx, canonical); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeAdminRegistered,
		sdk.NewAttribute(types.AttributeKeyModerator, moderator),
		sdk.NewAttribute(types.AttributeKeyAdmin, canonical),
	)
	return nil
}

func (k Keeper) RemoveAdmin(ctx context.Context, moderator, admin string) error {
	if err := k.validateModerator(ctx, moderator); err != nil {
		return err
	}
	canonical, _, err := k.canonicalAddress(admin)
	if err != nil {
		return types.ErrInvalidRequest.Wrapf("invalid admin address: %v", err)
	}
	has, err := k.admins.Has(ctx, canonical)
	if err != nil {
		return err
	}
	if !has {
		return types.ErrAdminNotFound.Wrap("admin is not registered")
	}
	if err := k.admins.Remove(ctx, canonical); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeAdminRemoved,
		sdk.NewAttribute(types.AttributeKeyModerator, moderator),
		sdk.NewAttribute(types.AttributeKeyAdmin, canonical),
	)
	return nil
}

func (k Keeper) validateModerator(ctx context.Context, moderator string) error {
	currentModerator, err := k.constitutionKeeper.GetModeratorAddress(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrInvalidModerator.Wrap("moderator_address is not initialized")
		}
		return err
	}
	expected, _, err := k.canonicalAddress(currentModerator)
	if err != nil {
		return types.ErrInvalidModerator.Wrapf("invalid configured moderator address: %v", err)
	}
	actual, _, err := k.canonicalAddress(moderator)
	if err != nil {
		return types.ErrInvalidModerator.Wrapf("invalid moderator address: %v", err)
	}
	if actual != expected {
		return types.ErrInvalidModerator.Wrap("invalid moderator")
	}
	return nil
}

func (k Keeper) IsAdmin(ctx context.Context, admin string) (bool, error) {
	canonical, _, err := k.canonicalAddress(admin)
	if err != nil {
		return false, types.ErrInvalidRequest.Wrapf("invalid admin address: %v", err)
	}
	return k.admins.Has(ctx, canonical)
}

func (k Keeper) requireRegisteredAdmin(ctx context.Context, admin string) (string, sdk.AccAddress, error) {
	canonical, addr, err := k.canonicalAddress(admin)
	if err != nil {
		return "", nil, types.ErrInvalidRequest.Wrapf("invalid admin address: %v", err)
	}
	has, err := k.admins.Has(ctx, canonical)
	if err != nil {
		return "", nil, err
	}
	if !has {
		return "", nil, types.ErrAdminNotFound.Wrap("admin is not registered")
	}
	return canonical, addr, nil
}

func (k Keeper) requireExchangeAdmin(ctx context.Context, exchange *bexv1.Exchange, signer string) (string, sdk.AccAddress, error) {
	canonical, addr, err := k.requireRegisteredAdmin(ctx, signer)
	if err != nil {
		return "", nil, err
	}
	if canonical != exchange.GetAdminAddress() {
		return "", nil, types.ErrWrongExchangeAdmin.Wrap("signer is not exchange admin")
	}
	return canonical, addr, nil
}

func (k Keeper) RegisterExchange(ctx context.Context, req *bexv1.MsgRegisterExchange) (*bexv1.Exchange, error) {
	admin, _, err := k.requireRegisteredAdmin(ctx, req.GetAdminAddress())
	if err != nil {
		return nil, err
	}
	id, err := k.nextID(ctx)
	if err != nil {
		return nil, err
	}
	reserveAddr := k.GetReserveAddress(ctx, id)
	reserveAddress, err := k.accountCodec.BytesToString(reserveAddr)
	if err != nil {
		return nil, err
	}

	ibcDenomA, err := buildIBCDenom(req.GetDenomA(), req.GetPortA(), req.GetChannelA())
	if err != nil {
		return nil, err
	}
	ibcDenomB, err := buildIBCDenom(req.GetDenomB(), req.GetPortB(), req.GetChannelB())
	if err != nil {
		return nil, err
	}
	status := req.GetStatus()
	if status == bexv1.ExchangeStatus_EXCHANGE_STATUS_UNSPECIFIED {
		status = bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE
	}
	exchange := &bexv1.Exchange{
		Id:                        id,
		AdminAddress:              admin,
		ReserveAddress:            reserveAddress,
		DenomA:                    strings.TrimSpace(req.GetDenomA()),
		PortA:                     strings.TrimSpace(req.GetPortA()),
		ChannelA:                  strings.TrimSpace(req.GetChannelA()),
		IbcDenomA:                 ibcDenomA,
		DenomB:                    strings.TrimSpace(req.GetDenomB()),
		PortB:                     strings.TrimSpace(req.GetPortB()),
		ChannelB:                  strings.TrimSpace(req.GetChannelB()),
		IbcDenomB:                 ibcDenomB,
		OracleSymbolAToB:          strings.TrimSpace(req.GetOracleSymbolAToB()),
		OracleSymbolBToA:          strings.TrimSpace(req.GetOracleSymbolBToA()),
		FeeBpsAToB:                req.GetFeeBpsAToB(),
		FeeBpsBToA:                req.GetFeeBpsBToA(),
		LimitAToB:                 normalizeIntString(req.GetLimitAToB()),
		LimitBToA:                 normalizeIntString(req.GetLimitBToA()),
		VolumeCapAToB:             normalizeIntString(req.GetVolumeCapAToB()),
		VolumeCapBToA:             normalizeIntString(req.GetVolumeCapBToA()),
		Revision:                  1,
		Status:                    status,
		Metadata:                  sortedMetadataCopy(req.GetMetadata()),
		VolumeEpochSeconds:        req.GetVolumeEpochSeconds(),
		MaxOracleStalenessSeconds: req.GetMaxOracleStalenessSeconds(),
	}
	if err := validateMutableExchangeConfig(exchange); err != nil {
		return nil, err
	}
	if err := k.validateActiveRoutes(ctx, exchange); err != nil {
		return nil, err
	}
	if k.accountKeeper.GetAccount(ctx, reserveAddr) == nil {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, reserveAddr))
	}
	if err := k.exchanges.Set(ctx, id, exchange); err != nil {
		return nil, err
	}
	if err := k.exchangesByAdmin.Set(ctx, collections.Join(admin, id)); err != nil {
		return nil, err
	}
	if err := k.reserveByAddress.Set(ctx, reserveAddress, id); err != nil {
		return nil, err
	}
	if err := k.collectedFees.Set(ctx, id, coinsToLedger(sdk.Coins{})); err != nil {
		return nil, err
	}
	if err := k.lockedFees.Set(ctx, id, coinsToLedger(sdk.Coins{})); err != nil {
		return nil, err
	}
	emitEvent(
		ctx,
		types.EventTypeExchangeRegistered,
		exchangeIDAttr(id),
		sdk.NewAttribute(types.AttributeKeyAdmin, exchange.GetAdminAddress()),
		sdk.NewAttribute(types.AttributeKeyReserveAddress, exchange.GetReserveAddress()),
		sdk.NewAttribute(types.AttributeKeyStatus, exchange.GetStatus().String()),
		uint64Attr(types.AttributeKeyRevision, exchange.GetRevision()),
	)
	return exchange, nil
}

func normalizeIntString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "0"
	}
	return value
}

func (k Keeper) GetExchange(ctx context.Context, exchangeID uint64) (*bexv1.Exchange, error) {
	exchange, err := k.exchanges.Get(ctx, exchangeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrExchangeNotFound.Wrapf("exchange %d not found", exchangeID)
		}
		return nil, err
	}
	return exchange, nil
}

func (k Keeper) GetActiveExchange(ctx context.Context, exchangeID uint64) (*bexv1.Exchange, error) {
	exchange, err := k.GetExchange(ctx, exchangeID)
	if err != nil {
		return nil, err
	}
	if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
		return nil, types.ErrExchangeDeleted.Wrapf("exchange %d is deleted", exchangeID)
	}
	return exchange, nil
}

func (k Keeper) UpdateExchange(ctx context.Context, signer string, exchangeID, expectedRevision uint64, patch *bexv1.ExchangeUpdatePatch) (*bexv1.Exchange, error) {
	if patch == nil || patchIsEmpty(patch) {
		return nil, types.ErrNoOpUpdate.Wrap("empty patch")
	}
	current, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return nil, err
	}
	if _, _, err := k.requireExchangeAdmin(ctx, current, signer); err != nil {
		return nil, err
	}
	if current.GetRevision() != expectedRevision {
		return nil, types.ErrRevisionConflict.Wrapf("expected %d, got %d", expectedRevision, current.GetRevision())
	}
	updated := cloneExchange(current)
	routeChanged := false

	if v := patch.GetDenomA(); v != nil {
		updated.DenomA = strings.TrimSpace(v.GetValue())
		routeChanged = true
	}
	if v := patch.GetPortA(); v != nil {
		updated.PortA = strings.TrimSpace(v.GetValue())
		routeChanged = true
	}
	if v := patch.GetChannelA(); v != nil {
		updated.ChannelA = strings.TrimSpace(v.GetValue())
		routeChanged = true
	}
	if v := patch.GetDenomB(); v != nil {
		updated.DenomB = strings.TrimSpace(v.GetValue())
		routeChanged = true
	}
	if v := patch.GetPortB(); v != nil {
		updated.PortB = strings.TrimSpace(v.GetValue())
		routeChanged = true
	}
	if v := patch.GetChannelB(); v != nil {
		updated.ChannelB = strings.TrimSpace(v.GetValue())
		routeChanged = true
	}
	if current.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE && routeChanged {
		return nil, types.ErrInvalidRoute.Wrap("route fields cannot change while active")
	}
	if routeChanged {
		updated.IbcDenomA, err = buildIBCDenom(updated.GetDenomA(), updated.GetPortA(), updated.GetChannelA())
		if err != nil {
			return nil, err
		}
		updated.IbcDenomB, err = buildIBCDenom(updated.GetDenomB(), updated.GetPortB(), updated.GetChannelB())
		if err != nil {
			return nil, err
		}
	}
	if v := patch.GetOracleSymbolAToB(); v != nil {
		updated.OracleSymbolAToB = strings.TrimSpace(v.GetValue())
	}
	if v := patch.GetOracleSymbolBToA(); v != nil {
		updated.OracleSymbolBToA = strings.TrimSpace(v.GetValue())
	}
	if v := patch.GetFeeBpsAToB(); v != nil {
		updated.FeeBpsAToB = v.GetValue()
	}
	if v := patch.GetFeeBpsBToA(); v != nil {
		updated.FeeBpsBToA = v.GetValue()
	}
	if v := patch.GetLimitAToB(); v != nil {
		updated.LimitAToB = normalizeIntString(v.GetValue())
	}
	if v := patch.GetLimitBToA(); v != nil {
		updated.LimitBToA = normalizeIntString(v.GetValue())
	}
	if v := patch.GetVolumeCapAToB(); v != nil {
		updated.VolumeCapAToB = normalizeIntString(v.GetValue())
	}
	if v := patch.GetVolumeCapBToA(); v != nil {
		updated.VolumeCapBToA = normalizeIntString(v.GetValue())
	}
	if v := patch.GetStatus(); v != nil {
		updated.Status = v.GetStatus()
	}
	if v := patch.GetClearMetadata(); v != nil && v.GetValue() {
		updated.Metadata = nil
	}
	if len(patch.GetMetadata()) > 0 {
		if updated.Metadata == nil {
			updated.Metadata = map[string]string{}
		}
		for key, value := range patch.GetMetadata() {
			updated.Metadata[key] = value
		}
		updated.Metadata = sortedMetadataCopy(updated.Metadata)
	}
	if v := patch.GetVolumeEpochSeconds(); v != nil {
		updated.VolumeEpochSeconds = v.GetValue()
	}
	if v := patch.GetPendingVolumeEpochSeconds(); v != nil {
		updated.PendingVolumeEpochSeconds = v.GetValue()
	}
	if v := patch.GetPendingVolumeEpochEffectiveAtUnix(); v != nil {
		updated.PendingVolumeEpochEffectiveAtUnix = v.GetValue()
	}
	if v := patch.GetMaxOracleStalenessSeconds(); v != nil {
		updated.MaxOracleStalenessSeconds = v.GetValue()
	}
	if proto.Equal(current, updated) {
		return nil, types.ErrNoOpUpdate.Wrap("patch does not change exchange")
	}
	if err := validateMutableExchangeConfig(updated); err != nil {
		return nil, err
	}
	if current.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE &&
		updated.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
		if err := k.validateActiveRoutes(ctx, updated); err != nil {
			return nil, err
		}
	}
	nextRevision, err := incrementRevision(updated.GetRevision())
	if err != nil {
		return nil, err
	}
	updated.Revision = nextRevision
	if err := k.exchanges.Set(ctx, exchangeID, updated); err != nil {
		return nil, err
	}
	emitEvent(
		ctx,
		types.EventTypeExchangeUpdated,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAdmin, updated.GetAdminAddress()),
		sdk.NewAttribute(types.AttributeKeyStatus, updated.GetStatus().String()),
		uint64Attr(types.AttributeKeyRevision, updated.GetRevision()),
	)
	if current.GetStatus() != updated.GetStatus() {
		emitEvent(
			ctx,
			types.EventTypeExchangeStatus,
			exchangeIDAttr(exchangeID),
			sdk.NewAttribute(types.AttributeKeyPreviousStatus, current.GetStatus().String()),
			sdk.NewAttribute(types.AttributeKeyStatus, updated.GetStatus().String()),
			uint64Attr(types.AttributeKeyRevision, updated.GetRevision()),
		)
	}
	return updated, nil
}

func patchIsEmpty(patch *bexv1.ExchangeUpdatePatch) bool {
	return patch.GetDenomA() == nil &&
		patch.GetPortA() == nil &&
		patch.GetChannelA() == nil &&
		patch.GetDenomB() == nil &&
		patch.GetPortB() == nil &&
		patch.GetChannelB() == nil &&
		patch.GetOracleSymbolAToB() == nil &&
		patch.GetOracleSymbolBToA() == nil &&
		patch.GetFeeBpsAToB() == nil &&
		patch.GetFeeBpsBToA() == nil &&
		patch.GetLimitAToB() == nil &&
		patch.GetLimitBToA() == nil &&
		patch.GetVolumeCapAToB() == nil &&
		patch.GetVolumeCapBToA() == nil &&
		patch.GetStatus() == nil &&
		len(patch.GetMetadata()) == 0 &&
		patch.GetClearMetadata() == nil &&
		patch.GetVolumeEpochSeconds() == nil &&
		patch.GetPendingVolumeEpochSeconds() == nil &&
		patch.GetPendingVolumeEpochEffectiveAtUnix() == nil &&
		patch.GetMaxOracleStalenessSeconds() == nil
}

func cloneExchange(exchange *bexv1.Exchange) *bexv1.Exchange {
	if exchange == nil {
		return nil
	}
	copied := proto.Clone(exchange).(*bexv1.Exchange)
	copied.Metadata = sortedMetadataCopy(copied.GetMetadata())
	return copied
}

func incrementRevision(revision uint64) (uint64, error) {
	if revision == 0 {
		return 0, types.ErrRevisionConflict.Wrap("exchange revision must be non-zero")
	}
	if revision == ^uint64(0) {
		return 0, types.ErrRevisionConflict.Wrap("exchange revision is exhausted")
	}
	return revision + 1, nil
}

func (k Keeper) DeleteExchange(ctx context.Context, signer string, exchangeID uint64) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if _, _, err := k.requireExchangeAdmin(ctx, exchange, signer); err != nil {
		return err
	}
	if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE {
		return types.ErrInvalidRequest.Wrap("exchange must be inactive before delete")
	}
	reserveAddr, err := k.accountCodec.StringToBytes(exchange.GetReserveAddress())
	if err != nil {
		return types.ErrInvalidRoute.Wrapf("invalid reserve address: %v", err)
	}
	if !k.bankKeeper.GetAllBalances(ctx, sdk.AccAddress(reserveAddr)).IsZero() {
		return types.ErrInsufficientReserve.Wrap("reserve balance must be zero before delete")
	}
	collected, err := k.GetCollectedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	locked, err := k.GetLockedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if !collected.IsZero() || !locked.IsZero() {
		return types.ErrInsufficientAvailableFees.Wrap("fee ledgers must be zero before delete")
	}
	deleted := cloneExchange(exchange)
	deleted.Status = bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED
	nextRevision, err := incrementRevision(deleted.GetRevision())
	if err != nil {
		return err
	}
	deleted.Revision = nextRevision
	if err := k.exchanges.Set(ctx, exchangeID, deleted); err != nil {
		return err
	}
	if err := k.exchangesByAdmin.Remove(ctx, collections.Join(exchange.GetAdminAddress(), exchangeID)); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeExchangeDeleted,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAdmin, exchange.GetAdminAddress()),
		sdk.NewAttribute(types.AttributeKeyReserveAddress, exchange.GetReserveAddress()),
		uint64Attr(types.AttributeKeyRevision, deleted.GetRevision()),
	)
	return nil
}
