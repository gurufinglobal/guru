package keeper

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/gurufinglobal/guru/v3/x/constitution/types"
)

func (k Keeper) GetMinGasPriceSchedule(ctx context.Context) (*constitutionv1.MinGasPriceSchedule, error) {
	return k.minGasPriceSchedule.Get(ctx)
}

func (k Keeper) GetCurrentMinGasPrice(ctx context.Context) (sdkmath.LegacyDec, error) {
	if k.feeMarket == nil {
		return sdkmath.LegacyDec{}, types.ErrFeeMarketKeeperMissing
	}

	params := k.feeMarket.GetParams(sdk.UnwrapSDKContext(ctx))
	return params.MinGasPrice, nil
}

func (k Keeper) SetMinGasPriceSchedule(ctx context.Context, schedule *constitutionv1.MinGasPriceSchedule) error {
	if err := k.ValidateMinGasPriceSchedule(schedule); err != nil {
		return err
	}

	return k.minGasPriceSchedule.Set(ctx, schedule)
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
	delayBlocks := sourceSubmissionInterval
	// The target oracle task's submission_interval is an operational UX rule:
	// validators should keep it stable enough for users to predict the pending
	// fee, but the on-chain apply delay is capped so refreshed data cannot wait
	// indefinitely behind an overly large interval.
	if delayBlocks > types.MinGasPricePendingDelayCap {
		delayBlocks = types.MinGasPricePendingDelayCap
	}

	replaced := false
	previousEffectiveHeight := int64(0)
	if existing, err := k.minGasPriceSchedule.Get(ctx); err == nil {
		replaced = true
		previousEffectiveHeight = existing.GetEffectiveHeight()
	} else if !isNotFound(err) {
		return err
	}

	schedule := &constitutionv1.MinGasPriceSchedule{
		EffectiveHeight:          value.GetBlockHeight() + int64(delayBlocks),
		MinGasPrice:              clampedMinGasPrice.String(),
		SourceSymbol:             normalizeMinGasPriceSymbol(value.GetSymbol()),
		SourceValue:              value.GetValue(),
		SourceOracleHeight:       value.GetBlockHeight(),
		SourceSubmissionInterval: sourceSubmissionInterval,
		PendingDelayBlocks:       delayBlocks,
		PendingDelayCapBlocks:    types.MinGasPricePendingDelayCap,
		RawMinGasPrice:           rawMinGasPrice.String(),
		PreviousMinGasPrice:      currentMinGasPrice.String(),
		ClampedMinGasPrice:       clampedMinGasPrice.String(),
	}
	if err := k.SetMinGasPriceSchedule(ctx, schedule); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMinGasPriceUpdateScheduled,
		sdk.NewAttribute(types.AttributeKeySourceSymbol, schedule.GetSourceSymbol()),
		sdk.NewAttribute(types.AttributeKeySourceValue, schedule.GetSourceValue()),
		sdk.NewAttribute(types.AttributeKeySourceOracleHeight, strconv.FormatInt(schedule.GetSourceOracleHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeySourceSubmissionInterval, strconv.FormatUint(uint64(schedule.GetSourceSubmissionInterval()), 10)),
		sdk.NewAttribute(types.AttributeKeyPendingDelayBlocks, strconv.FormatUint(uint64(schedule.GetPendingDelayBlocks()), 10)),
		sdk.NewAttribute(types.AttributeKeyPendingDelayCapBlocks, strconv.FormatUint(uint64(schedule.GetPendingDelayCapBlocks()), 10)),
		sdk.NewAttribute(types.AttributeKeyScheduledHeight, strconv.FormatInt(sdkCtx.BlockHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyEffectiveHeight, strconv.FormatInt(schedule.GetEffectiveHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyPreviousEffectiveHeight, strconv.FormatInt(previousEffectiveHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyPreviousMinGasPrice, schedule.GetPreviousMinGasPrice()),
		sdk.NewAttribute(types.AttributeKeyRawMinGasPrice, schedule.GetRawMinGasPrice()),
		sdk.NewAttribute(types.AttributeKeyClampedMinGasPrice, schedule.GetClampedMinGasPrice()),
		sdk.NewAttribute(types.AttributeKeyScaleFactor, types.MinGasPriceScaleFactor),
		sdk.NewAttribute(types.AttributeKeyClampPPM, strconv.FormatUint(uint64(types.MinGasPriceClampPPM), 10)),
		sdk.NewAttribute(types.AttributeKeyReplaced, strconv.FormatBool(replaced)),
	))

	return nil
}

func (k Keeper) ApplyDueMinGasPriceSchedule(ctx context.Context) error {
	schedule, err := k.minGasPriceSchedule.Get(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	nextHeight := sdkCtx.BlockHeight() + 1
	if schedule.GetEffectiveHeight() > nextHeight {
		return nil
	}
	if schedule.GetEffectiveHeight() < nextHeight {
		k.emitMinGasPriceSkipped(ctx, "stale_pending_schedule", oracleValueFromSchedule(schedule), "")
		return k.ClearMinGasPriceSchedule(ctx)
	}
	if err := k.ValidateMinGasPriceSchedule(schedule); err != nil {
		k.emitMinGasPriceSkipped(ctx, "invalid_pending_schedule", oracleValueFromSchedule(schedule), "")
		return k.ClearMinGasPriceSchedule(ctx)
	}

	newMinGasPrice, err := parsePositiveDec(schedule.GetMinGasPrice(), "min_gas_price")
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

	params := k.feeMarket.GetParams(sdkCtx)
	params.MinGasPrice = newMinGasPrice
	if err := params.Validate(); err != nil {
		return types.ErrInvalidMinGasPrice.Wrapf("invalid feemarket params: %v", err)
	}
	if err := k.feeMarket.SetParams(sdkCtx, params); err != nil {
		return err
	}
	if err := k.ClearMinGasPriceSchedule(ctx); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeMinGasPriceUpdateApplied,
		sdk.NewAttribute(types.AttributeKeyApplyHeight, strconv.FormatInt(sdkCtx.BlockHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyEffectiveHeight, strconv.FormatInt(schedule.GetEffectiveHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyPreviousMinGasPrice, currentMinGasPrice.String()),
		sdk.NewAttribute(types.AttributeKeyNewMinGasPrice, newMinGasPrice.String()),
		sdk.NewAttribute(types.AttributeKeySourceSymbol, schedule.GetSourceSymbol()),
		sdk.NewAttribute(types.AttributeKeySourceOracleHeight, strconv.FormatInt(schedule.GetSourceOracleHeight(), 10)),
	))

	return nil
}

func (k Keeper) ValidateMinGasPriceSchedule(schedule *constitutionv1.MinGasPriceSchedule) error {
	if schedule == nil {
		return types.ErrInvalidMinGasPrice.Wrap("schedule cannot be nil")
	}
	if schedule.GetEffectiveHeight() <= 0 {
		return types.ErrInvalidMinGasPrice.Wrap("effective_height must be positive")
	}
	minGasPrice, err := parsePositiveDec(schedule.GetMinGasPrice(), "min_gas_price")
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
	if schedule.GetSourceSubmissionInterval() == 0 {
		return types.ErrInvalidMinGasPrice.Wrap("source_submission_interval must be positive")
	}
	expectedDelayBlocks := schedule.GetSourceSubmissionInterval()
	if expectedDelayBlocks > types.MinGasPricePendingDelayCap {
		expectedDelayBlocks = types.MinGasPricePendingDelayCap
	}
	if schedule.GetPendingDelayBlocks() == 0 {
		return types.ErrInvalidMinGasPrice.Wrap("pending_delay_blocks must be positive")
	}
	if schedule.GetPendingDelayBlocks() != expectedDelayBlocks {
		return types.ErrInvalidMinGasPrice.Wrap("pending_delay_blocks must equal min(source_submission_interval, pending_delay_cap_blocks)")
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
	clamped, err := parsePositiveDec(schedule.GetClampedMinGasPrice(), "clamped_min_gas_price")
	if err != nil {
		return err
	}
	expectedClampedMinGasPrice := clampMinGasPrice(rawMinGasPrice, previousMinGasPrice)
	if !clamped.Equal(expectedClampedMinGasPrice) {
		return types.ErrInvalidMinGasPrice.Wrap("clamped_min_gas_price does not match raw_min_gas_price and previous_min_gas_price")
	}
	if !minGasPrice.Equal(clamped) {
		return types.ErrInvalidMinGasPrice.Wrap("min_gas_price must equal clamped_min_gas_price")
	}

	return nil
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

func oracleValueFromSchedule(schedule *constitutionv1.MinGasPriceSchedule) *oraclev1.OracleValue {
	if schedule == nil {
		return nil
	}

	return &oraclev1.OracleValue{
		Symbol:      schedule.GetSourceSymbol(),
		Value:       schedule.GetSourceValue(),
		BlockHeight: schedule.GetSourceOracleHeight(),
	}
}

func calculateRawMinGasPrice(priceAtoms sdkmath.Int) sdkmath.Int {
	scaleFactor := mustIntFromString(types.MinGasPriceScaleFactor)
	pricePrecision := mustIntFromString(types.MinGasPriceOraclePricePrecision)
	return scaleFactor.Mul(pricePrecision).Quo(priceAtoms)
}

func clampMinGasPrice(raw sdkmath.Int, current sdkmath.LegacyDec) sdkmath.LegacyDec {
	rawDec := raw.ToLegacyDec()
	lower := current.MulInt64(int64(types.SeparationRatioScalePPM - types.MinGasPriceClampPPM)).QuoInt64(int64(types.SeparationRatioScalePPM))
	upper := current.MulInt64(int64(types.SeparationRatioScalePPM + types.MinGasPriceClampPPM)).QuoInt64(int64(types.SeparationRatioScalePPM))
	switch {
	case rawDec.LT(lower):
		return lower
	case rawDec.GT(upper):
		return upper
	default:
		return rawDec
	}
}

func parsePositiveDec(value string, fieldName string) (sdkmath.LegacyDec, error) {
	parsed, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(value))
	if err != nil || parsed.IsNil() || !parsed.IsPositive() {
		return sdkmath.LegacyDec{}, types.ErrInvalidMinGasPrice.Wrapf("%s must be a positive decimal string", fieldName)
	}
	return parsed, nil
}

func parseNonNegativeInt(value string, fieldName string) (sdkmath.Int, error) {
	parsed, ok := sdkmath.NewIntFromString(strings.TrimSpace(value))
	if !ok || parsed.IsNegative() {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrapf("%s must be a non-negative integer string", fieldName)
	}
	return parsed, nil
}

func parsePositiveOraclePriceAtoms(value string) (sdkmath.Int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price cannot be empty")
	}
	if strings.HasPrefix(trimmed, "-") {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price must be positive")
	}
	trimmed = strings.TrimPrefix(trimmed, "+")

	parts := strings.Split(trimmed, ".")
	if len(parts) > 2 {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price has too many decimal points")
	}

	integer := parts[0]
	fractional := ""
	if len(parts) == 2 {
		fractional = parts[1]
	}
	if integer == "" && fractional == "" {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price cannot be empty")
	}
	if integer == "" {
		integer = "0"
	}
	if !isDecimalDigits(integer) || !isDecimalDigits(fractional) {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price must contain only decimal digits")
	}
	if len(fractional) > 18 {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price precision cannot exceed 18 decimal places")
	}
	fractional += strings.Repeat("0", 18-len(fractional))

	atoms, ok := sdkmath.NewIntFromString(trimLeadingDecimalZeros(integer + fractional))
	if !ok || !atoms.IsPositive() {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price must be positive")
	}
	return atoms, nil
}

func isDecimalDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func trimLeadingDecimalZeros(value string) string {
	trimmed := strings.TrimLeft(value, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

func normalizeMinGasPriceSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func mustIntFromString(value string) sdkmath.Int {
	parsed, ok := sdkmath.NewIntFromString(value)
	if !ok {
		panic(fmt.Sprintf("invalid integer constant %q", value))
	}
	return parsed
}

func isNotFound(err error) bool {
	return errors.Is(err, collections.ErrNotFound)
}
