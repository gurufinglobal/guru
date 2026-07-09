package keeper

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
)

const (
	defaultMinGasPriceDec = "630000000000.000000000000000000"
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
	_, err := f.keeper.GetMinGasPriceSchedule(dueCtx)
	require.ErrorIs(t, err, collections.ErrNotFound)

	events := dueCtx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, constitutiontypes.EventTypeMinGasPriceUpdateApplied, events[0].Type)
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyEffectiveHeight, "30"))
	require.True(t, eventHasAttribute(events[0], constitutiontypes.AttributeKeyNewMinGasPrice, upperClampedDec))
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

func TestValidateMinGasPriceScheduleRejectsInconsistentDerivedFields(t *testing.T) {
	f := setupKeeperFixture(t)
	valid := &constitutionv1.MinGasPriceSchedule{
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

	rawMismatch := *valid
	rawMismatch.RawMinGasPrice = "1"
	require.ErrorContains(t, f.keeper.ValidateMinGasPriceSchedule(&rawMismatch), "raw_min_gas_price does not match")

	delayMismatch := *valid
	delayMismatch.PendingDelayBlocks = 4
	delayMismatch.EffectiveHeight = 24
	require.ErrorContains(t, f.keeper.ValidateMinGasPriceSchedule(&delayMismatch), "pending_delay_blocks must equal")

	clampMismatch := *valid
	clampMismatch.ScheduledMinGasPrice = "1"
	require.ErrorContains(t, f.keeper.ValidateMinGasPriceSchedule(&clampMismatch), "scheduled_min_gas_price does not match")
}

func TestQueryServerMinGasPriceReturnsCurrentAndPending(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	queryServer := NewQueryServer(&f.keeper)

	resp, err := queryServer.MinGasPrice(ctx, &constitutionv1.QueryMinGasPriceRequest{})
	require.NoError(t, err)
	require.Equal(t, defaultMinGasPriceDec, resp.GetCurrentMinGasPrice())
	require.Nil(t, resp.GetPending())

	require.NoError(t, f.keeper.AfterOracleValueApplied(ctx, testOracleValue(appparams.MinGasPriceOracleSymbol, "1.0", 20), 2))

	resp, err = queryServer.MinGasPrice(ctx, &constitutionv1.QueryMinGasPriceRequest{})
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

func eventHasAttribute(event sdk.Event, key, value string) bool {
	for _, attr := range event.Attributes {
		if attr.Key == key && attr.Value == value {
			return true
		}
	}
	return false
}
