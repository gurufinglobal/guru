package keeper

import (
	"context"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v2/config"
	"github.com/gurufinglobal/guru/v2/x/constitution/types"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func (k Keeper) emitMinGasPriceScheduled(
	ctx context.Context,
	schedule *types.MinGasPriceSchedule,
	scheduledHeight int64,
	previousEffectiveHeight int64,
	replaced bool,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMinGasPriceUpdateScheduled,
		sdk.NewAttribute(types.AttributeKeySourceSymbol, schedule.GetSourceSymbol()),
		sdk.NewAttribute(types.AttributeKeySourceValue, schedule.GetSourceValue()),
		sdk.NewAttribute(types.AttributeKeySourceOracleHeight, strconv.FormatInt(schedule.GetSourceOracleHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeySourceSubmissionInterval, strconv.FormatUint(uint64(schedule.GetSourceSubmissionIntervalBlocks()), 10)),
		sdk.NewAttribute(types.AttributeKeyPendingDelayBlocks, strconv.FormatUint(uint64(schedule.GetPendingDelayBlocks()), 10)),
		sdk.NewAttribute(types.AttributeKeyPendingDelayCapBlocks, strconv.FormatUint(uint64(schedule.GetPendingDelayCapBlocks()), 10)),
		sdk.NewAttribute(types.AttributeKeyScheduledHeight, strconv.FormatInt(scheduledHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyEffectiveHeight, strconv.FormatInt(schedule.GetEffectiveHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyPreviousEffectiveHeight, strconv.FormatInt(previousEffectiveHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyPreviousMinGasPrice, schedule.GetPreviousMinGasPrice()),
		sdk.NewAttribute(types.AttributeKeyRawMinGasPrice, schedule.GetRawMinGasPrice()),
		sdk.NewAttribute(types.AttributeKeyClampedMinGasPrice, schedule.GetScheduledMinGasPrice()),
		sdk.NewAttribute(types.AttributeKeyScaleFactor, types.MinGasPriceScaleFactor),
		sdk.NewAttribute(types.AttributeKeyClampPPM, strconv.FormatUint(uint64(types.MinGasPriceClampPPM), 10)),
		sdk.NewAttribute(types.AttributeKeyReplaced, strconv.FormatBool(replaced)),
	))
}

func (k Keeper) emitMinGasPriceApplied(
	ctx context.Context,
	schedule *types.MinGasPriceSchedule,
	previousMinGasPrice string,
	newMinGasPrice string,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMinGasPriceUpdateApplied,
		sdk.NewAttribute(types.AttributeKeyApplyHeight, strconv.FormatInt(sdkCtx.BlockHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyEffectiveHeight, strconv.FormatInt(schedule.GetEffectiveHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyPreviousMinGasPrice, previousMinGasPrice),
		sdk.NewAttribute(types.AttributeKeyNewMinGasPrice, newMinGasPrice),
		sdk.NewAttribute(types.AttributeKeySourceSymbol, schedule.GetSourceSymbol()),
		sdk.NewAttribute(types.AttributeKeySourceOracleHeight, strconv.FormatInt(schedule.GetSourceOracleHeight(), 10)),
	))
}

func (k Keeper) emitMinGasPriceSkipped(ctx context.Context, reason string, value *oraclev1.OracleValue, currentMinGasPrice string) {
	sourceSymbol := appparams.MinGasPriceOracleSymbol
	sourceValue := ""
	sourceOracleHeight := int64(0)
	if value != nil {
		sourceSymbol = normalizeMinGasPriceSymbol(value.GetSymbol())
		sourceValue = value.GetValue()
		sourceOracleHeight = value.GetBlockHeight()
	}
	if currentMinGasPrice == "" && k.feeMarket != nil {
		if current, err := k.GetCurrentMinGasPrice(ctx); err == nil {
			currentMinGasPrice = current.String()
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMinGasPriceUpdateSkipped,
		sdk.NewAttribute(types.AttributeKeyHeight, strconv.FormatInt(sdkCtx.BlockHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyReason, reason),
		sdk.NewAttribute(types.AttributeKeySourceSymbol, sourceSymbol),
		sdk.NewAttribute(types.AttributeKeySourceValue, sourceValue),
		sdk.NewAttribute(types.AttributeKeySourceOracleHeight, strconv.FormatInt(sourceOracleHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyCurrentMinGasPrice, currentMinGasPrice),
	))
}

func oracleValueFromSchedule(schedule *types.MinGasPriceSchedule) *oraclev1.OracleValue {
	if schedule == nil {
		return nil
	}

	return &oraclev1.OracleValue{
		Symbol:      schedule.GetSourceSymbol(),
		Value:       schedule.GetSourceValue(),
		BlockHeight: schedule.GetSourceOracleHeight(),
	}
}
