package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v2/config"
	"github.com/gurufinglobal/guru/v2/x/constitution/types"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

type minGasPriceScheduleTiming uint8

const (
	minGasPriceScheduleMissed minGasPriceScheduleTiming = iota
	minGasPriceScheduleDueNextBlock
	minGasPriceScheduleFuture
)

func (k Keeper) GetMinGasPriceSchedule(ctx context.Context) (*types.MinGasPriceSchedule, error) {
	schedule, err := k.minGasPriceSchedule.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &schedule, nil
}

func (k Keeper) GetCurrentMinGasPrice(ctx context.Context) (sdkmath.LegacyDec, error) {
	if k.feeMarket == nil {
		return sdkmath.LegacyDec{}, types.ErrFeeMarketKeeperMissing
	}

	return k.feeMarket.GetMinGasPrice(ctx), nil
}

func (k Keeper) SetMinGasPriceSchedule(ctx context.Context, schedule *types.MinGasPriceSchedule) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.ValidateMinGasPriceScheduleAtHeight(schedule, sdkCtx.BlockHeight()); err != nil {
		return err
	}

	return k.minGasPriceSchedule.Set(ctx, *schedule)
}

func (k Keeper) ClearMinGasPriceSchedule(ctx context.Context) error {
	return k.minGasPriceSchedule.Remove(ctx)
}

func (k Keeper) AfterOracleValueApplied(ctx context.Context, value *oraclev1.OracleValue, sourceSubmissionInterval uint32) error {
	if value == nil {
		return nil
	}
	if normalizeMinGasPriceSymbol(value.GetSymbol()) != normalizeMinGasPriceSymbol(appparams.MinGasPriceOracleSymbol) {
		return nil
	}
	if sourceSubmissionInterval == 0 {
		k.emitMinGasPriceSkipped(ctx, "invalid_submission_interval", value, "")
		return nil
	}
	if value.GetBlockHeight() <= 0 {
		k.emitMinGasPriceSkipped(ctx, "invalid_source_oracle_height", value, "")
		return nil
	}

	priceAtoms, err := parsePositiveOraclePriceAtoms(value.GetValue())
	if err != nil {
		k.emitMinGasPriceSkipped(ctx, "invalid_oracle_price", value, "")
		return nil
	}
	if !priceAtoms.IsPositive() {
		k.emitMinGasPriceSkipped(ctx, "non_positive_oracle_price", value, "")
		return nil
	}

	currentMinGasPrice, err := k.GetCurrentMinGasPrice(ctx)
	if err != nil {
		return err
	}
	if !currentMinGasPrice.IsPositive() {
		k.emitMinGasPriceSkipped(ctx, "non_positive_current_min_gas_price", value, currentMinGasPrice.String())
		return nil
	}

	rawMinGasPrice := calculateRawMinGasPrice(priceAtoms)
	clampedMinGasPrice := clampMinGasPrice(rawMinGasPrice, currentMinGasPrice)
	if !clampedMinGasPrice.IsPositive() {
		k.emitMinGasPriceSkipped(ctx, "non_positive_computed_min_gas_price", value, currentMinGasPrice.String())
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	delayBlocks := pendingDelayBlocksForInterval(sourceSubmissionInterval)

	schedule := &types.MinGasPriceSchedule{
		EffectiveHeight:                value.GetBlockHeight() + int64(delayBlocks),
		ScheduledMinGasPrice:           clampedMinGasPrice.String(),
		SourceSymbol:                   normalizeMinGasPriceSymbol(value.GetSymbol()),
		SourceValue:                    value.GetValue(),
		SourceOracleHeight:             value.GetBlockHeight(),
		SourceSubmissionIntervalBlocks: sourceSubmissionInterval,
		PendingDelayBlocks:             delayBlocks,
		PendingDelayCapBlocks:          types.MinGasPricePendingDelayCap,
		RawMinGasPrice:                 rawMinGasPrice.String(),
		PreviousMinGasPrice:            currentMinGasPrice.String(),
	}
	if err := k.ValidateMinGasPriceScheduleAtHeight(schedule, sdkCtx.BlockHeight()); err != nil {
		return err
	}

	replaced := false
	previousEffectiveHeight := int64(0)
	if existing, err := k.minGasPriceSchedule.Get(ctx); err == nil {
		if classifyMinGasPriceSchedule(existing.GetEffectiveHeight(), sdkCtx.BlockHeight()) == minGasPriceScheduleMissed {
			if err := k.handleMissedMinGasPriceSchedule(ctx, &existing); err != nil {
				return err
			}
		} else {
			replaced = true
			previousEffectiveHeight = existing.GetEffectiveHeight()
		}
	} else if !isNotFound(err) {
		return err
	}

	if err := k.SetMinGasPriceSchedule(ctx, schedule); err != nil {
		return err
	}

	k.emitMinGasPriceScheduled(ctx, schedule, sdkCtx.BlockHeight(), previousEffectiveHeight, replaced)

	return nil
}

func (k Keeper) ApplyDueMinGasPriceSchedule(ctx context.Context) error {
	schedule, err := k.GetMinGasPriceSchedule(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	switch classifyMinGasPriceSchedule(schedule.GetEffectiveHeight(), sdkCtx.BlockHeight()) {
	case minGasPriceScheduleFuture:
		return nil
	case minGasPriceScheduleMissed:
		return k.handleMissedMinGasPriceSchedule(ctx, schedule)
	case minGasPriceScheduleDueNextBlock:
	}
	if err := k.ValidateMinGasPriceSchedule(schedule); err != nil {
		k.emitMinGasPriceSkipped(ctx, "invalid_pending_schedule", oracleValueFromSchedule(schedule), "")
		return k.ClearMinGasPriceSchedule(ctx)
	}

	newMinGasPrice, err := parsePositiveDec(schedule.GetScheduledMinGasPrice(), "scheduled_min_gas_price")
	if err != nil {
		k.emitMinGasPriceSkipped(ctx, "invalid_pending_min_gas_price", oracleValueFromSchedule(schedule), "")
		return k.ClearMinGasPriceSchedule(ctx)
	}
	scheduledPreviousMinGasPrice, err := parsePositiveDec(schedule.GetPreviousMinGasPrice(), "previous_min_gas_price")
	if err != nil {
		k.emitMinGasPriceSkipped(ctx, "invalid_pending_previous_min_gas_price", oracleValueFromSchedule(schedule), "")
		return k.ClearMinGasPriceSchedule(ctx)
	}
	currentMinGasPrice, err := k.GetCurrentMinGasPrice(ctx)
	if err != nil {
		return err
	}
	if !currentMinGasPrice.Equal(scheduledPreviousMinGasPrice) {
		k.emitMinGasPriceSkipped(ctx, "current_min_gas_price_changed", oracleValueFromSchedule(schedule), currentMinGasPrice.String())
		return k.ClearMinGasPriceSchedule(ctx)
	}

	if err := k.feeMarket.SetMinGasPrice(ctx, newMinGasPrice); err != nil {
		return err
	}
	if err := k.ClearMinGasPriceSchedule(ctx); err != nil {
		return err
	}

	k.emitMinGasPriceApplied(ctx, schedule, currentMinGasPrice.String(), newMinGasPrice.String())

	return nil
}

func (k Keeper) ValidateMinGasPriceSchedule(schedule *types.MinGasPriceSchedule) error {
	if schedule == nil {
		return types.ErrInvalidMinGasPrice.Wrap("schedule cannot be nil")
	}
	if schedule.GetEffectiveHeight() <= 0 {
		return types.ErrInvalidMinGasPrice.Wrap("effective_height must be positive")
	}
	scheduledMinGasPrice, err := parsePositiveDec(schedule.GetScheduledMinGasPrice(), "scheduled_min_gas_price")
	if err != nil {
		return err
	}
	if schedule.GetSourceSymbol() != normalizeMinGasPriceSymbol(appparams.MinGasPriceOracleSymbol) {
		return types.ErrInvalidMinGasPrice.Wrapf("source_symbol must be %q", appparams.MinGasPriceOracleSymbol)
	}
	priceAtoms, err := parsePositiveOraclePriceAtoms(schedule.GetSourceValue())
	if err != nil {
		return err
	}
	if schedule.GetSourceOracleHeight() <= 0 {
		return types.ErrInvalidMinGasPrice.Wrap("source_oracle_height must be positive")
	}
	if schedule.GetSourceSubmissionIntervalBlocks() == 0 {
		return types.ErrInvalidMinGasPrice.Wrap("source_submission_interval_blocks must be positive")
	}
	expectedDelayBlocks := pendingDelayBlocksForInterval(schedule.GetSourceSubmissionIntervalBlocks())
	if schedule.GetPendingDelayBlocks() == 0 {
		return types.ErrInvalidMinGasPrice.Wrap("pending_delay_blocks must be positive")
	}
	if schedule.GetPendingDelayBlocks() != expectedDelayBlocks {
		return types.ErrInvalidMinGasPrice.Wrap("pending_delay_blocks must equal min(source_submission_interval_blocks, pending_delay_cap_blocks)")
	}
	if schedule.GetPendingDelayBlocks() > types.MinGasPricePendingDelayCap {
		return types.ErrInvalidMinGasPrice.Wrapf("pending_delay_blocks cannot exceed %d", types.MinGasPricePendingDelayCap)
	}
	if schedule.GetPendingDelayCapBlocks() != types.MinGasPricePendingDelayCap {
		return types.ErrInvalidMinGasPrice.Wrapf("pending_delay_cap_blocks must be %d", types.MinGasPricePendingDelayCap)
	}
	if schedule.GetEffectiveHeight() != schedule.GetSourceOracleHeight()+int64(schedule.GetPendingDelayBlocks()) {
		return types.ErrInvalidMinGasPrice.Wrap("effective_height must equal source_oracle_height plus pending_delay_blocks")
	}
	rawMinGasPrice, err := parseNonNegativeInt(schedule.GetRawMinGasPrice(), "raw_min_gas_price")
	if err != nil {
		return err
	}
	expectedRawMinGasPrice := calculateRawMinGasPrice(priceAtoms)
	if !rawMinGasPrice.Equal(expectedRawMinGasPrice) {
		return types.ErrInvalidMinGasPrice.Wrap("raw_min_gas_price does not match source_value and scale factor")
	}
	previousMinGasPrice, err := parsePositiveDec(schedule.GetPreviousMinGasPrice(), "previous_min_gas_price")
	if err != nil {
		return err
	}
	expectedClampedMinGasPrice := clampMinGasPrice(rawMinGasPrice, previousMinGasPrice)
	if !scheduledMinGasPrice.Equal(expectedClampedMinGasPrice) {
		return types.ErrInvalidMinGasPrice.Wrap("scheduled_min_gas_price does not match raw_min_gas_price and previous_min_gas_price")
	}

	return nil
}

// ValidateMinGasPriceScheduleAtHeight validates a schedule and requires its
// effective height to remain in the future relative to currentHeight.
func (k Keeper) ValidateMinGasPriceScheduleAtHeight(schedule *types.MinGasPriceSchedule, currentHeight int64) error {
	if err := k.ValidateMinGasPriceSchedule(schedule); err != nil {
		return err
	}
	if classifyMinGasPriceSchedule(schedule.GetEffectiveHeight(), currentHeight) == minGasPriceScheduleMissed {
		return types.ErrInvalidMinGasPrice.Wrapf(
			"effective_height %d must be greater than current block height %d",
			schedule.GetEffectiveHeight(),
			currentHeight,
		)
	}

	return nil
}

func classifyMinGasPriceSchedule(effectiveHeight, currentHeight int64) minGasPriceScheduleTiming {
	switch {
	case effectiveHeight <= currentHeight:
		return minGasPriceScheduleMissed
	case effectiveHeight == currentHeight+1:
		return minGasPriceScheduleDueNextBlock
	default:
		return minGasPriceScheduleFuture
	}
}

func (k Keeper) handleMissedMinGasPriceSchedule(ctx context.Context, schedule *types.MinGasPriceSchedule) error {
	current, err := k.GetCurrentMinGasPrice(ctx)
	if err != nil {
		return err
	}
	currentMinGasPrice := current.String()

	k.emitMissedMinGasPriceSchedule(ctx, schedule, currentMinGasPrice)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	k.Logger(ctx).Warn(
		"discarding missed minimum gas price schedule",
		types.AttributeKeyReason, types.MinGasPriceUpdateReasonMissedEffectiveHeight,
		types.AttributeKeyHeight, sdkCtx.BlockHeight(),
		types.AttributeKeyObservedHeight, sdkCtx.BlockHeight(),
		types.AttributeKeyNextHeight, sdkCtx.BlockHeight()+1,
		types.AttributeKeyEffectiveHeight, schedule.GetEffectiveHeight(),
		types.AttributeKeyCurrentMinGasPrice, currentMinGasPrice,
		types.AttributeKeyScheduledMinGasPrice, schedule.GetScheduledMinGasPrice(),
		types.AttributeKeyPendingMinGasPrice, schedule.GetScheduledMinGasPrice(),
		types.AttributeKeySourceOracleHeight, schedule.GetSourceOracleHeight(),
		types.AttributeKeyPendingDelayBlocks, schedule.GetPendingDelayBlocks(),
	)

	return k.ClearMinGasPriceSchedule(ctx)
}
