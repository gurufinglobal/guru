package keeper

import (
	"context"
	"errors"
	"math/big"
	"strconv"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

const maxVolumePruneRowsPerRecord = 32

var legacyDecPrecision = new(big.Int).Exp(big.NewInt(10), big.NewInt(sdkmath.LegacyPrecision), nil)

func validateOracleFreshness(blockTime time.Time, updatedAt int64, maxStalenessSeconds uint32) error {
	if blockTime.IsZero() {
		blockTime = time.Unix(0, 0)
	}
	now := blockTime.Unix()
	if updatedAt <= 0 || updatedAt > now {
		return types.ErrStaleOracleRate.Wrap("oracle rate timestamp is outside the valid block-time range")
	}
	if maxStalenessSeconds > 0 && now-updatedAt > int64(maxStalenessSeconds) {
		return types.ErrStaleOracleRate.Wrap("oracle rate is stale")
	}
	return nil
}

func checkedVolumeAdd(used, amount sdkmath.Int) (sdkmath.Int, error) {
	next, err := used.SafeAdd(amount)
	if err != nil {
		return sdkmath.Int{}, types.ErrVolumeCapExceeded.Wrap("volume usage exceeds uint256 max")
	}
	return next, nil
}

func (k Keeper) ResolveSwapDirection(ctx context.Context, exchangeID uint64, inputDenom string) (bexv1.SwapDirection, error) {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, err
	}
	switch inputDenom {
	case exchange.GetDenomA():
		return bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, nil
	case exchange.GetDenomB():
		return bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A, nil
	default:
		return bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, types.ErrInvalidRoute.Wrap("input denom does not match exchange")
	}
}

func (k Keeper) QuoteSwap(ctx context.Context, req *bexv1.QuoteSwapRequest) (*bexv1.QuoteSwapResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty quote request")
	}
	exchange, err := k.GetActiveExchange(ctx, req.GetExchangeId())
	if err != nil {
		return nil, err
	}
	if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
		return nil, types.ErrInvalidRoute.Wrap("exchange is not active")
	}
	if err := k.validateActiveRoutes(ctx, exchange); err != nil {
		return nil, err
	}
	amountIn, err := validateRequiredIntString("amount_in", req.GetAmountIn())
	if err != nil {
		return nil, err
	}
	if !amountIn.IsPositive() {
		return nil, types.ErrInvalidRequest.Wrap("amount_in must be positive")
	}
	direction, err := k.ResolveSwapDirection(ctx, req.GetExchangeId(), req.GetInputDenom())
	if err != nil {
		return nil, err
	}
	oracleSymbol, outputDenom, feeBps, outputLimit, volumeCap, err := quoteConfig(exchange, direction)
	if err != nil {
		return nil, err
	}
	rateValue, err := k.oracleKeeper.GetLatestValue(ctx, oracleSymbol)
	if err != nil {
		return nil, types.ErrInvalidOracleRate.Wrapf("oracle value for %q not found: %v", oracleSymbol, err)
	}
	rate, err := sdkmath.LegacyNewDecFromStr(rateValue.GetValue())
	if err != nil || !rate.IsPositive() {
		return nil, types.ErrInvalidOracleRate.Wrapf("invalid oracle rate %q", rateValue.GetValue())
	}
	if err := validateOracleFreshness(
		sdk.UnwrapSDKContext(ctx).BlockTime(),
		rateValue.GetBlockTimeUnix(),
		exchange.GetMaxOracleStalenessSeconds(),
	); err != nil {
		return nil, err
	}
	fee := ceilFee(amountIn, feeBps)
	netIn := amountIn.Sub(fee)
	if !netIn.IsPositive() {
		return nil, types.ErrInvalidRequest.Wrap("net input is zero after fee")
	}
	amountOut, err := quoteAmountOut(rate, netIn)
	if err != nil {
		return nil, err
	}
	if !amountOut.IsPositive() {
		return nil, types.ErrInvalidOracleRate.Wrap("quote output is zero")
	}
	if !outputLimit.IsZero() && amountOut.GT(outputLimit) {
		return nil, types.ErrOutputLimitExceeded.Wrap("quote output exceeds limit")
	}
	used, err := k.GetCurrentVolumeAmount(ctx, exchange, direction)
	if err != nil {
		return nil, err
	}
	nextVolume, err := checkedVolumeAdd(used, amountOut)
	if err != nil {
		return nil, err
	}
	if !volumeCap.IsZero() && nextVolume.GT(volumeCap) {
		return nil, types.ErrVolumeCapExceeded.Wrap("quote output exceeds volume cap")
	}
	return &bexv1.QuoteSwapResponse{
		ExchangeId:   exchange.GetId(),
		Direction:    direction,
		InputDenom:   req.GetInputDenom(),
		OutputDenom:  outputDenom,
		OracleSymbol: oracleSymbol,
		OracleRate:   rate.String(),
		AmountIn:     amountIn.String(),
		FeeAmount:    fee.String(),
		NetAmountIn:  netIn.String(),
		AmountOut:    amountOut.String(),
		OutputLimit:  outputLimit.String(),
		VolumeCap:    volumeCap.String(),
		VolumeUsed:   used.String(),
		MinAmountIn:  minAmountIn(rate, feeBps).String(),
	}, nil
}

func quoteConfig(exchange *bexv1.Exchange, direction bexv1.SwapDirection) (string, string, uint32, sdkmath.Int, sdkmath.Int, error) {
	switch direction {
	case bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B:
		limit, err := validateExchangeLimitIntString("limit_a_to_b", exchange.GetLimitAToB())
		if err != nil {
			return "", "", 0, sdkmath.Int{}, sdkmath.Int{}, err
		}
		cap, err := validateExchangeLimitIntString("volume_cap_a_to_b", exchange.GetVolumeCapAToB())
		if err != nil {
			return "", "", 0, sdkmath.Int{}, sdkmath.Int{}, err
		}
		return exchange.GetOracleSymbolAToB(), exchange.GetIbcDenomB(), exchange.GetFeeBpsAToB(), limit, cap, nil
	case bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A:
		limit, err := validateExchangeLimitIntString("limit_b_to_a", exchange.GetLimitBToA())
		if err != nil {
			return "", "", 0, sdkmath.Int{}, sdkmath.Int{}, err
		}
		cap, err := validateExchangeLimitIntString("volume_cap_b_to_a", exchange.GetVolumeCapBToA())
		if err != nil {
			return "", "", 0, sdkmath.Int{}, sdkmath.Int{}, err
		}
		return exchange.GetOracleSymbolBToA(), exchange.GetIbcDenomA(), exchange.GetFeeBpsBToA(), limit, cap, nil
	default:
		return "", "", 0, sdkmath.Int{}, sdkmath.Int{}, types.ErrInvalidRoute.Wrap("invalid direction")
	}
}

func ceilFee(amount sdkmath.Int, bps uint32) sdkmath.Int {
	if bps == 0 {
		return sdkmath.ZeroInt()
	}
	fee := new(big.Int).Mul(amount.BigInt(), new(big.Int).SetUint64(uint64(bps)))
	fee.Add(fee, big.NewInt(9999))
	fee.Quo(fee, big.NewInt(10000))
	return sdkmath.NewIntFromBigInt(fee)
}

func minAmountIn(rate sdkmath.LegacyDec, bps uint32) sdkmath.Int {
	low := sdkmath.OneInt()
	high := maxUint256Int
	rateScaled := rate.BigInt()
	if !quoteOutputsPositive(rateScaled, high, bps) {
		return sdkmath.ZeroInt()
	}
	for low.LT(high) {
		mid := low.Add(high.Sub(low).QuoRaw(2))
		if quoteOutputsPositive(rateScaled, mid, bps) {
			high = mid
		} else {
			low = mid.AddRaw(1)
		}
	}
	return low
}

func quoteOutputsPositive(rateScaled *big.Int, amount sdkmath.Int, bps uint32) bool {
	net := amount.Sub(ceilFee(amount, bps))
	if !net.IsPositive() {
		return false
	}
	out := new(big.Int).Mul(rateScaled, net.BigInt())
	return out.Cmp(legacyDecPrecision) >= 0
}

func quoteAmountOut(rate sdkmath.LegacyDec, netIn sdkmath.Int) (sdkmath.Int, error) {
	out := new(big.Int).Mul(rate.BigInt(), netIn.BigInt())
	out.Quo(out, legacyDecPrecision)
	if out.BitLen() > 256 {
		return sdkmath.Int{}, types.ErrInvalidOracleRate.Wrap("quote output exceeds uint256 max")
	}
	return sdkmath.NewIntFromBigInt(out), nil
}

func (k Keeper) GetCurrentVolumeAmount(ctx context.Context, exchange *bexv1.Exchange, direction bexv1.SwapDirection) (sdkmath.Int, error) {
	key := currentVolumeKey(sdk.UnwrapSDKContext(ctx).BlockTime(), exchange.GetId(), direction, effectiveVolumeEpochSeconds(exchange, sdk.UnwrapSDKContext(ctx).BlockTime()))
	amount, _, err := k.getVolumeWindowAmount(ctx, key)
	return amount, err
}

func (k Keeper) RecordVolumeWindow(ctx context.Context, exchangeID uint64, direction bexv1.SwapDirection, amountOut sdkmath.Int) error {
	return executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		return k.recordVolumeWindow(cacheCtx, exchangeID, direction, amountOut)
	})
}

func (k Keeper) recordVolumeWindow(ctx context.Context, exchangeID uint64, direction bexv1.SwapDirection, amountOut sdkmath.Int) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
		return types.ErrInvalidRoute.Wrap("volume recording requires an active exchange")
	}
	if amountOut.IsNil() || !amountOut.IsPositive() {
		return types.ErrInvalidRequest.Wrap("amount_out must be positive")
	}
	exchange, err = k.activatePendingVolumeEpoch(ctx, exchange)
	if err != nil {
		return err
	}
	_, _, _, _, cap, err := quoteConfig(exchange, direction)
	if err != nil {
		return err
	}
	key := currentVolumeKey(sdk.UnwrapSDKContext(ctx).BlockTime(), exchangeID, direction, effectiveVolumeEpochSeconds(exchange, sdk.UnwrapSDKContext(ctx).BlockTime()))
	used, found, err := k.getVolumeWindowAmount(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		if err := k.pruneExpiredVolumeWindows(ctx, exchangeID, direction, maxVolumePruneRowsPerRecord); err != nil {
			return err
		}
	}
	next, err := checkedVolumeAdd(used, amountOut)
	if err != nil {
		return err
	}
	if !cap.IsZero() && next.GT(cap) {
		emitEvent(
			ctx,
			types.EventTypeVolumeCapExceeded,
			exchangeIDAttr(exchangeID),
			directionAttr(direction),
			intAttr(types.AttributeKeyCurrentAmount, used),
			intAttr(types.AttributeKeyNextAmount, next),
			intAttr(types.AttributeKeyCap, cap),
		)
		return types.ErrVolumeCapExceeded.Wrap("recording would exceed volume cap")
	}
	if err := k.volumeWindow.Set(ctx, key, next.String()); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeVolumeRecorded,
		exchangeIDAttr(exchangeID),
		directionAttr(direction),
		uint64Attr("epoch_start_unix", key.K3()),
		sdk.NewAttribute("epoch_seconds", strconv.FormatUint(uint64(key.K4()), 10)),
		intAttr(types.AttributeKeyAmount, amountOut),
		intAttr(types.AttributeKeyCurrentAmount, next),
	)
	return nil
}

func (k Keeper) getVolumeWindowAmount(ctx context.Context, key volumeWindowKey) (sdkmath.Int, bool, error) {
	value, err := k.volumeWindow.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return sdkmath.ZeroInt(), false, nil
		}
		return sdkmath.Int{}, false, err
	}
	amount, err := validateRequiredIntString("volume_window.amount", value)
	if err != nil {
		return sdkmath.Int{}, false, err
	}
	return amount, true, nil
}

func effectiveVolumeEpochSeconds(exchange *bexv1.Exchange, blockTime time.Time) uint32 {
	if !pendingVolumeEpochDue(exchange, blockTime) {
		return exchange.GetVolumeEpochSeconds()
	}
	return exchange.GetPendingVolumeEpochSeconds()
}

func pendingVolumeEpochDue(exchange *bexv1.Exchange, blockTime time.Time) bool {
	if exchange.GetPendingVolumeEpochSeconds() == 0 || exchange.GetPendingVolumeEpochEffectiveAtUnix() == 0 {
		return false
	}
	if blockTime.IsZero() || blockTime.Unix() < 0 {
		return false
	}
	return uint64(blockTime.Unix()) >= exchange.GetPendingVolumeEpochEffectiveAtUnix()
}

func (k Keeper) activatePendingVolumeEpoch(ctx context.Context, exchange *bexv1.Exchange) (*bexv1.Exchange, error) {
	blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	if !pendingVolumeEpochDue(exchange, blockTime) {
		return exchange, nil
	}
	updated := cloneExchange(exchange)
	updated.VolumeEpochSeconds = updated.GetPendingVolumeEpochSeconds()
	updated.PendingVolumeEpochSeconds = 0
	updated.PendingVolumeEpochEffectiveAtUnix = 0
	nextRevision, err := incrementRevision(updated.GetRevision())
	if err != nil {
		return nil, err
	}
	updated.Revision = nextRevision
	if err := k.exchanges.Set(ctx, updated.GetId(), updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (k Keeper) pruneExpiredVolumeWindows(ctx context.Context, exchangeID uint64, direction bexv1.SwapDirection, limit int) error {
	if limit <= 0 {
		return nil
	}
	blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	if blockTime.IsZero() || blockTime.Unix() <= 0 {
		return nil
	}
	now := uint64(blockTime.Unix())
	cursorKey := collections.Join(exchangeID, uint32(direction))
	cursor, err := k.volumePruneCursor.Get(ctx, cursorKey)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		cursor = 0
	}

	scanned := 0
	nextCursor := cursor
	keysToDelete := make([]volumeWindowKey, 0, limit)
	rng := collections.NewSuperPrefixedQuadRange[uint64, uint32, uint64, uint32](exchangeID, uint32(direction))
	err = k.volumeWindow.Walk(ctx, rng, func(key volumeWindowKey, _ string) (bool, error) {
		if key.K3() < cursor {
			return false, nil
		}
		if scanned >= limit {
			return true, nil
		}
		scanned++
		nextCursor = key.K3()
		if volumeWindowExpired(key, now) {
			keysToDelete = append(keysToDelete, key)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	for _, key := range keysToDelete {
		if err := k.volumeWindow.Remove(ctx, key); err != nil {
			return err
		}
	}
	if scanned == 0 {
		return nil
	}
	return k.volumePruneCursor.Set(ctx, cursorKey, nextCursor)
}

func volumeWindowExpired(key volumeWindowKey, now uint64) bool {
	if key.K4() == 0 || key.K3() > now {
		return false
	}
	return uint64(key.K4()) <= now-key.K3()
}

func currentVolumeKey(blockTime time.Time, exchangeID uint64, direction bexv1.SwapDirection, epochSeconds uint32) volumeWindowKey {
	if epochSeconds == 0 {
		epochSeconds = 1
	}
	unix := uint64(0)
	if !blockTime.IsZero() && blockTime.Unix() > 0 {
		unix = uint64(blockTime.Unix())
	}
	epoch := unix / uint64(epochSeconds) * uint64(epochSeconds)
	return collections.Join4(exchangeID, uint32(direction), epoch, epochSeconds)
}
