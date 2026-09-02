package keeper

import (
	"bytes"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmante "github.com/cosmos/evm/ante/evm"
	appparams "github.com/gurufinglobal/guru/v2/config"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
)

const (
	defaultMinGasPriceDec = "630000000000.000000000000000000"
	lowerClampedDec       = "567000000000.000000000000000000"
	upperClampedDec       = "693000000000.000000000000000000"
)

func TestAfterOracleValueAppliedSchedulesCappedPendingMinGasPrice(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())

	err := f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "0.10345", 20), 50)
	require.NoError(t, err)

	schedule, err := f.keeper.GetMinGasPriceSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(30), schedule.GetEffectiveHeight())
	require.Equal(t, uint32(10), schedule.GetPendingDelayBlocks())
	require.Equal(t, uint32(10), schedule.GetPendingDelayCapBlocks())
	require.Equal(t, defaultMinGasPriceDec, schedule.GetPreviousMinGasPrice())
	require.Equal(t, "6089898501691", schedule.GetRawMinGasPrice())
	require.Equal(t, upperClampedDec, schedule.GetScheduledMinGasPrice())

	events := ctx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateScheduled, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyPendingDelayBlocks, "10"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyReplaced, "false"))
}

func TestAfterOracleValueAppliedClampsBothDirectionsAndPreservesInRangeValue(t *testing.T) {
	tests := []struct {
		name      string
		price     string
		raw       string
		scheduled string
	}{
		{name: "lower clamp", price: "2.0", raw: "315000000000", scheduled: lowerClampedDec},
		{name: "within range", price: "1.05", raw: "600000000000", scheduled: "600000000000.000000000000000000"},
		{name: "upper clamp", price: "0.5", raw: "1260000000000", scheduled: upperClampedDec},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())

			require.NoError(t, f.keeper.AfterOracleValueApplied(
				ctx,
				testOracleValue(appparams.MinGasPriceOracleSymbol, tc.price, 20),
				1,
			))

			schedule, err := f.keeper.GetMinGasPriceSchedule(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.raw, schedule.GetRawMinGasPrice())
			require.Equal(t, tc.scheduled, schedule.GetScheduledMinGasPrice())
		})
	}
}

func TestApplyDueMinGasPriceScheduleAppliesForNextBlock(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "0.10345", 20), 50))

	notDueCtx := ctx.WithBlockHeight(28).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.ApplyDueMinGasPriceSchedule(notDueCtx))
	require.Equal(t, defaultMinGasPriceDec, f.feeMarketKeeper.GetParams(notDueCtx).MinGasPrice.String())

	dueCtx := ctx.WithBlockHeight(29).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.ApplyDueMinGasPriceSchedule(dueCtx))
	require.Equal(t, upperClampedDec, f.feeMarketKeeper.GetParams(dueCtx).MinGasPrice.String())
	require.Equal(t, 1, f.feeMarketKeeper.setMinGasPriceCalls)
	_, err := f.keeper.GetMinGasPriceSchedule(dueCtx)
	require.ErrorIs(t, err, collections.ErrNotFound)

	events := dueCtx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateApplied, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyEffectiveHeight, "30"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyNewMinGasPrice, upperClampedDec))
}

func TestApplyDueMinGasPriceScheduleClearsMissedSchedule(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "0.5", 20), 2))

	var warningLog bytes.Buffer
	missedCtx := ctx.WithBlockHeight(22).
		WithEventManager(sdk.NewEventManager()).
		WithLogger(log.NewLogger(&warningLog, log.OutputJSONOption()))
	require.NoError(t, f.keeper.ApplyDueMinGasPriceSchedule(missedCtx))
	require.Equal(t, defaultMinGasPriceDec, f.feeMarketKeeper.GetParams(missedCtx).MinGasPrice.String())
	require.Zero(t, f.feeMarketKeeper.setMinGasPriceCalls)
	_, err := f.keeper.GetMinGasPriceSchedule(missedCtx)
	require.ErrorIs(t, err, collections.ErrNotFound)

	events := missedCtx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateSkipped, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyReason, constitutiontypes.MinGasPriceUpdateReasonMissedEffectiveHeight))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyHeight, "22"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyObservedHeight, "22"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyNextHeight, "23"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyEffectiveHeight, "22"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyCurrentMinGasPrice, defaultMinGasPriceDec))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyScheduledMinGasPrice, upperClampedDec))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyClampedMinGasPrice, upperClampedDec))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyPendingMinGasPrice, upperClampedDec))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeySourceOracleHeight, "20"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyPendingDelayBlocks, "2"))

	logOutput := warningLog.String()
	require.Contains(t, logOutput, `"level":"warn"`)
	require.Contains(t, logOutput, `"reason":"missed_effective_height"`)
	require.Contains(t, logOutput, `"observed_height":22`)
	require.Contains(t, logOutput, `"effective_height":22`)
	require.Contains(t, logOutput, `"next_height":23`)
	require.Contains(t, logOutput, `"scheduled_min_gas_price":"`+upperClampedDec+`"`)
	require.Contains(t, logOutput, `"pending_min_gas_price":"`+upperClampedDec+`"`)

	var repeatedWarningLog bytes.Buffer
	nextCtx := missedCtx.WithBlockHeight(23).
		WithEventManager(sdk.NewEventManager()).
		WithLogger(log.NewLogger(&repeatedWarningLog, log.OutputJSONOption()))
	require.NoError(t, f.keeper.ApplyDueMinGasPriceSchedule(nextCtx))
	require.Empty(t, nextCtx.EventManager().Events())
	require.Empty(t, repeatedWarningLog.String())
	require.Zero(t, f.feeMarketKeeper.setMinGasPriceCalls)
}

func TestAppliedMinGasPriceControlsEVMGlobalFee(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	fee := mustTestDec("650000000000")
	gasLimit := mustTestDec("1")

	before, err := f.keeper.GetCurrentMinGasPrice(ctx)
	require.NoError(t, err)
	require.NoError(t, evmante.CheckGlobalFee(fee, before, gasLimit))

	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "0.5", 20), 2))
	dueCtx := ctx.WithBlockHeight(21).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.ApplyDueMinGasPriceSchedule(dueCtx))

	after, err := f.keeper.GetCurrentMinGasPrice(dueCtx)
	require.NoError(t, err)
	require.Equal(t, upperClampedDec, after.String())
	require.ErrorContains(t, evmante.CheckGlobalFee(fee, after, gasLimit), "minimum global fee")
}

func TestApplyDueMinGasPriceScheduleSkipsWhenCurrentMinGasPriceChanged(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "0.10345", 20), 10))

	params := f.feeMarketKeeper.GetParams(ctx)
	params.MinGasPrice = mustTestDec("630000000000.5")
	require.NoError(t, f.feeMarketKeeper.SetParams(ctx, params))

	dueCtx := ctx.WithBlockHeight(29).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.ApplyDueMinGasPriceSchedule(dueCtx))
	require.Equal(t, "630000000000.500000000000000000", f.feeMarketKeeper.GetParams(dueCtx).MinGasPrice.String())
	_, err := f.keeper.GetMinGasPriceSchedule(dueCtx)
	require.ErrorIs(t, err, collections.ErrNotFound)

	events := dueCtx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateSkipped, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyReason, "current_min_gas_price_changed"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeySourceOracleHeight, "20"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyCurrentMinGasPrice, "630000000000.500000000000000000"))
}

func TestAfterOracleValueAppliedReplacesSinglePendingSchedule(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "0.10345", 20), 10))

	ctx = ctx.WithBlockHeight(21).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "1.0", 21), 2))

	schedule, err := f.keeper.GetMinGasPriceSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(23), schedule.GetEffectiveHeight())
	require.Equal(t, int64(21), schedule.GetSourceOracleHeight())
	require.Equal(t, uint32(2), schedule.GetPendingDelayBlocks())
	require.Equal(t, defaultMinGasPriceDec, schedule.GetScheduledMinGasPrice())

	events := ctx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateScheduled, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyPreviousEffectiveHeight, "30"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyReplaced, "true"))
}

func TestAfterOracleValueAppliedDiagnosesMissedScheduleBeforeReplacement(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.AfterOracleValueApplied(
		ctx,
		testOracleValue(appparams.MinGasPriceOracleSymbol, "0.5", 20),
		2,
	))

	var warningLog bytes.Buffer
	replacementCtx := ctx.WithBlockHeight(22).
		WithEventManager(sdk.NewEventManager()).
		WithLogger(log.NewLogger(&warningLog, log.OutputJSONOption()))
	require.NoError(t, f.keeper.AfterOracleValueApplied(
		replacementCtx,
		testOracleValue(appparams.MinGasPriceOracleSymbol, "1.0", 22),
		2,
	))

	schedule, err := f.keeper.GetMinGasPriceSchedule(replacementCtx)
	require.NoError(t, err)
	require.Equal(t, int64(24), schedule.GetEffectiveHeight())
	require.Equal(t, int64(22), schedule.GetSourceOracleHeight())
	require.Equal(t, defaultMinGasPriceDec, schedule.GetScheduledMinGasPrice())
	require.Equal(t, defaultMinGasPriceDec, f.feeMarketKeeper.GetParams(replacementCtx).MinGasPrice.String())
	require.Zero(t, f.feeMarketKeeper.setMinGasPriceCalls)

	events := replacementCtx.EventManager().Events()
	require.Len(t, events, 2)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateSkipped, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyReason, constitutiontypes.MinGasPriceUpdateReasonMissedEffectiveHeight))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyEffectiveHeight, "22"))
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateScheduled, events[1].Type)
	require.True(t, eventHasAttribute(events[1], constitutiontypes.AttributeKeyPreviousEffectiveHeight, "0"))
	require.True(t, eventHasAttribute(events[1], constitutiontypes.AttributeKeyReplaced, "false"))
	require.Contains(t, warningLog.String(), `"reason":"missed_effective_height"`)
}

func TestSetMinGasPriceScheduleRequiresFutureEffectiveHeight(t *testing.T) {
	for _, tc := range []struct {
		name            string
		effectiveHeight int64
		wantErr         bool
	}{
		{name: "rejects effective height below current height", effectiveHeight: 19, wantErr: true},
		{name: "rejects effective height equal to current height", effectiveHeight: 20, wantErr: true},
		{name: "allows effective height at next block", effectiveHeight: 21, wantErr: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			ctx := f.ctx.WithBlockHeight(20)
			schedule := testMinGasPriceSchedule(tc.effectiveHeight-1, 1)

			err := f.keeper.SetMinGasPriceSchedule(ctx, schedule)
			if tc.wantErr {
				require.ErrorIs(t, err, constitutiontypes.ErrInvalidMinGasPrice)
				require.ErrorContains(t, err, "must be greater than current block height")
				_, getErr := f.keeper.GetMinGasPriceSchedule(ctx)
				require.ErrorIs(t, getErr, collections.ErrNotFound)
				return
			}

			require.NoError(t, err)
			stored, getErr := f.keeper.GetMinGasPriceSchedule(ctx)
			require.NoError(t, getErr)
			require.Equal(t, int64(21), stored.GetEffectiveHeight())
		})
	}
}

func TestAfterOracleValueAppliedRejectsNonFutureSchedule(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(22).WithEventManager(sdk.NewEventManager())

	err := f.keeper.AfterOracleValueApplied(
		ctx,
		testOracleValue(appparams.MinGasPriceOracleSymbol, "1.0", 20),
		2,
	)
	require.ErrorIs(t, err, constitutiontypes.ErrInvalidMinGasPrice)
	require.ErrorContains(t, err, "must be greater than current block height")
	_, getErr := f.keeper.GetMinGasPriceSchedule(ctx)
	require.ErrorIs(t, getErr, collections.ErrNotFound)
	require.Empty(t, ctx.EventManager().Events())
	require.Zero(t, f.feeMarketKeeper.setMinGasPriceCalls)
}

func TestAfterOracleValueAppliedSkipsInvalidTargetPrice(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())

	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "0", 20), 5))

	_, err := f.keeper.GetMinGasPriceSchedule(ctx)
	require.ErrorIs(t, err, collections.ErrNotFound)
	events := ctx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateSkipped, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyReason, "invalid_oracle_price"))
}

func TestAfterOracleValueAppliedIgnoresNonMinGasPriceSymbol(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())

	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue("BTC/USD", "0.10345", 20), 5))

	_, err := f.keeper.GetMinGasPriceSchedule(ctx)
	require.ErrorIs(t, err, collections.ErrNotFound)
	require.Empty(t, ctx.EventManager().Events())
}

func TestAfterOracleValueAppliedSkipsInvalidSchedulingInputs(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		value                    *oraclev1.OracleValue
		sourceSubmissionInterval uint32
		reason                   string
	}{
		{
			name:                     "zero submission interval",
			value:                    testOracleValue(appparams.MinGasPriceOracleSymbol, "0.10345", 20),
			sourceSubmissionInterval: 0,
			reason:                   "invalid_submission_interval",
		},
		{
			name:                     "zero source height",
			value:                    testOracleValue(appparams.MinGasPriceOracleSymbol, "0.10345", 0),
			sourceSubmissionInterval: 5,
			reason:                   "invalid_source_oracle_height",
		},
		{
			name:                     "negative source height",
			value:                    testOracleValue(appparams.MinGasPriceOracleSymbol, "0.10345", -1),
			sourceSubmissionInterval: 5,
			reason:                   "invalid_source_oracle_height",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())

			require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, tc.value, tc.sourceSubmissionInterval))

			_, err := f.keeper.GetMinGasPriceSchedule(ctx)
			require.ErrorIs(t, err, collections.ErrNotFound)
			events := ctx.EventManager().Events()
			require.Len(t, events, 1)
			require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateSkipped, events[0].Type)
			require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyReason, tc.reason))
		})
	}
}

func TestValidateMinGasPriceScheduleRejectsInconsistentDerivedFields(t *testing.T) {
	f := setupKeeperFixture(t)
	valid := &constitutiontypes.MinGasPriceSchedule{
		EffectiveHeight:                25,
		ScheduledMinGasPrice:           defaultMinGasPriceDec,
		SourceSymbol:                   appparams.MinGasPriceOracleSymbol,
		SourceValue:                    "1.0",
		SourceOracleHeight:             20,
		SourceSubmissionIntervalBlocks: 5,
		PendingDelayBlocks:             5,
		PendingDelayCapBlocks:          10,
		RawMinGasPrice:                 "630000000000",
		PreviousMinGasPrice:            defaultMinGasPriceDec,
	}
	require.NoError(t, f.keeper.ValidateMinGasPriceSchedule(valid))

	rawMismatchValue := *valid
	rawMismatch := &rawMismatchValue
	rawMismatch.RawMinGasPrice = "1"
	require.ErrorContains(t, f.keeper.ValidateMinGasPriceSchedule(rawMismatch), "raw_min_gas_price does not match")

	delayMismatchValue := *valid
	delayMismatch := &delayMismatchValue
	delayMismatch.PendingDelayBlocks = 4
	delayMismatch.EffectiveHeight = 24
	require.ErrorContains(t, f.keeper.ValidateMinGasPriceSchedule(delayMismatch), "pending_delay_blocks must equal")

	clampMismatchValue := *valid
	clampMismatch := &clampMismatchValue
	clampMismatch.ScheduledMinGasPrice = "1"
	require.ErrorContains(t, f.keeper.ValidateMinGasPriceSchedule(clampMismatch), "scheduled_min_gas_price does not match")
}

func TestQueryServerMinGasPriceReturnsCurrentAndPending(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	queryServer := NewQueryServer(&f.keeper)

	resp, err := queryServer.MinGasPrice(ctx, &constitutiontypes.QueryMinGasPriceRequest{})
	require.NoError(t, err)
	require.Equal(t, defaultMinGasPriceDec, resp.GetCurrentMinGasPrice())
	require.Nil(t, resp.GetPending())

	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "1.0", 20), 2))

	resp, err = queryServer.MinGasPrice(ctx, &constitutiontypes.QueryMinGasPriceRequest{})
	require.NoError(t, err)
	require.Equal(t, defaultMinGasPriceDec, resp.GetCurrentMinGasPrice())
	require.NotNil(t, resp.GetPending())
	require.Equal(t, int64(22), resp.GetPending().GetEffectiveHeight())
}

func testOracleValue(symbol, value string, height int64) *oraclev1.OracleValue {
	return &oraclev1.OracleValue{
		Symbol:      symbol,
		ValueType:   oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:       value,
		BlockHeight: height,
	}
}

func testMinGasPriceSchedule(sourceHeight int64, sourceSubmissionInterval uint32) *constitutiontypes.MinGasPriceSchedule {
	delayBlocks := pendingDelayBlocksForInterval(sourceSubmissionInterval)
	return &constitutiontypes.MinGasPriceSchedule{
		EffectiveHeight:                sourceHeight + int64(delayBlocks),
		ScheduledMinGasPrice:           defaultMinGasPriceDec,
		SourceSymbol:                   appparams.MinGasPriceOracleSymbol,
		SourceValue:                    "1.0",
		SourceOracleHeight:             sourceHeight,
		SourceSubmissionIntervalBlocks: sourceSubmissionInterval,
		PendingDelayBlocks:             delayBlocks,
		PendingDelayCapBlocks:          constitutiontypes.MinGasPricePendingDelayCap,
		RawMinGasPrice:                 constitutiontypes.MinGasPriceScaleFactor,
		PreviousMinGasPrice:            defaultMinGasPriceDec,
	}
}

func eventHasAttribute(event sdk.Event, key, value string) bool {
	for _, attr := range event.Attributes {
		if attr.Key == key && attr.Value == value {
			return true
		}
	}
	return false
}
