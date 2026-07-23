package keeper

import (
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestVolumeGenerationPreventsD1D2D1ReuseAcrossDirections(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	directions := []types.SwapDirection{
		types.SwapDirection_SWAP_DIRECTION_A_TO_B,
		types.SwapDirection_SWAP_DIRECTION_B_TO_A,
	}
	d1Amounts := map[types.SwapDirection]int64{
		types.SwapDirection_SWAP_DIRECTION_A_TO_B: 17,
		types.SwapDirection_SWAP_DIRECTION_B_TO_A: 23,
	}
	d2Amounts := map[types.SwapDirection]int64{
		types.SwapDirection_SWAP_DIRECTION_A_TO_B: 3,
		types.SwapDirection_SWAP_DIRECTION_B_TO_A: 5,
	}
	finalAmounts := map[types.SwapDirection]int64{
		types.SwapDirection_SWAP_DIRECTION_A_TO_B: 7,
		types.SwapDirection_SWAP_DIRECTION_B_TO_A: 11,
	}

	firstD1Keys := make(map[types.SwapDirection]volumeWindowKey, len(directions))
	for _, direction := range directions {
		firstD1Keys[direction] = currentVolumeKey(
			f.ctx.BlockTime(),
			exchange.GetId(),
			direction,
			exchange.GetVolumeEpochSeconds(),
			exchange.GetVolumeWindowGeneration(),
		)
		require.NoError(t, f.keeper.RecordVolumeWindow(
			f.ctx,
			exchange.GetId(),
			direction,
			sdkmath.NewInt(d1Amounts[direction]),
		))
	}

	d2 := minVolumeEpochSecs * 2
	d2Exchange, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&types.ExchangeUpdatePatch{VolumeEpochSeconds: types.NewUInt32Value(d2)},
	)
	require.NoError(t, err)
	require.Equal(t, exchange.GetVolumeWindowGeneration()+1, d2Exchange.GetVolumeWindowGeneration())
	for _, direction := range directions {
		require.NoError(t, f.keeper.RecordVolumeWindow(
			f.ctx,
			d2Exchange.GetId(),
			direction,
			sdkmath.NewInt(d2Amounts[direction]),
		))
	}

	finalD1Exchange, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		d2Exchange.GetId(),
		d2Exchange.GetRevision(),
		&types.ExchangeUpdatePatch{VolumeEpochSeconds: types.NewUInt32Value(minVolumeEpochSecs)},
	)
	require.NoError(t, err)
	require.Equal(t, d2Exchange.GetVolumeWindowGeneration()+1, finalD1Exchange.GetVolumeWindowGeneration())

	for _, direction := range directions {
		finalD1Key := currentVolumeKey(
			f.ctx.BlockTime(),
			finalD1Exchange.GetId(),
			direction,
			finalD1Exchange.GetVolumeEpochSeconds(),
			finalD1Exchange.GetVolumeWindowGeneration(),
		)
		require.Equal(t, firstD1Keys[direction].K1(), finalD1Key.K1(), "same aligned D1 expiry must be exercised")
		require.NotEqual(t, firstD1Keys[direction], finalD1Key, "generation must keep the repeated D1 key distinct")

		used, err := f.keeper.GetCurrentVolumeAmount(f.ctx, finalD1Exchange, direction)
		require.NoError(t, err)
		require.True(t, used.IsZero(), "re-entering D1 must not reuse generation-1 usage")

		require.NoError(t, f.keeper.RecordVolumeWindow(
			f.ctx,
			finalD1Exchange.GetId(),
			direction,
			sdkmath.NewInt(finalAmounts[direction]),
		))
		oldAmount, err := f.keeper.volumeWindow.Get(f.ctx, firstD1Keys[direction])
		require.NoError(t, err)
		require.Equal(t, strconv.FormatInt(d1Amounts[direction], 10), oldAmount)
		newAmount, err := f.keeper.volumeWindow.Get(f.ctx, finalD1Key)
		require.NoError(t, err)
		require.Equal(t, strconv.FormatInt(finalAmounts[direction], 10), newAmount)
	}
}

func TestSameDurationPendingQueryAndActivationUseNextGeneration(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := types.SwapDirection_SWAP_DIRECTION_A_TO_B
	oldKey := currentVolumeKey(
		f.ctx.BlockTime(),
		exchange.GetId(),
		direction,
		exchange.GetVolumeEpochSeconds(),
		exchange.GetVolumeWindowGeneration(),
	)
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), direction, sdkmath.NewInt(9)))

	effectiveAt := uint64(f.ctx.BlockTime().Unix())
	pending, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&types.ExchangeUpdatePatch{
			PendingVolumeEpochSeconds:         types.NewUInt32Value(exchange.GetVolumeEpochSeconds()),
			PendingVolumeEpochEffectiveAtUnix: types.NewUInt64Value(effectiveAt),
		},
	)
	require.NoError(t, err)
	require.Equal(t, exchange.GetVolumeWindowGeneration(), pending.GetVolumeWindowGeneration())

	beforeQuery, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	queryResponse, err := NewQueryServer(&f.keeper).VolumeWindow(f.ctx, &types.QueryVolumeWindowRequest{
		ExchangeId: exchange.GetId(),
		Direction:  direction,
	})
	require.NoError(t, err)
	require.Equal(t, "0", queryResponse.GetWindow().GetAmount())
	require.Equal(t, pending.GetVolumeEpochSeconds(), queryResponse.GetWindow().GetEpochSeconds())
	require.Equal(t, pending.GetVolumeWindowGeneration()+1, queryResponse.GetWindow().GetVolumeWindowGeneration())
	afterQuery, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(beforeQuery, afterQuery), "query must not persist due activation")

	eventCount := len(f.ctx.EventManager().Events())
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), direction, sdkmath.NewInt(4)))
	activated, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, pending.GetVolumeWindowGeneration()+1, activated.GetVolumeWindowGeneration())
	require.Equal(t, pending.GetRevision()+1, activated.GetRevision())
	require.Zero(t, activated.GetPendingVolumeEpochSeconds())
	require.Zero(t, activated.GetPendingVolumeEpochEffectiveAtUnix())

	newKey := currentVolumeKey(
		f.ctx.BlockTime(),
		activated.GetId(),
		direction,
		activated.GetVolumeEpochSeconds(),
		activated.GetVolumeWindowGeneration(),
	)
	oldAmount, err := f.keeper.volumeWindow.Get(f.ctx, oldKey)
	require.NoError(t, err)
	require.Equal(t, "9", oldAmount)
	newAmount, err := f.keeper.volumeWindow.Get(f.ctx, newKey)
	require.NoError(t, err)
	require.Equal(t, "4", newAmount)

	events := f.ctx.EventManager().Events()
	require.Len(t, events, eventCount+2)
	require.Equal(t, types.EventTypeVolumeEpochActivated, events[eventCount].Type)
	require.Equal(t, types.EventTypeVolumeRecorded, events[eventCount+1].Type)
	activationAttributes := make(map[string]string, len(events[eventCount].Attributes))
	for _, attribute := range events[eventCount].Attributes {
		activationAttributes[attribute.Key] = attribute.Value
	}
	require.Equal(t, strconv.FormatUint(uint64(pending.GetVolumeEpochSeconds()), 10), activationAttributes[types.AttributeKeyPreviousEpoch])
	require.Equal(t, strconv.FormatUint(uint64(pending.GetVolumeEpochSeconds()), 10), activationAttributes[types.AttributeKeyEpochSeconds])
	require.Equal(t, strconv.FormatUint(effectiveAt, 10), activationAttributes[types.AttributeKeyEffectiveAt])
	require.Equal(t, strconv.FormatUint(activated.GetVolumeWindowGeneration(), 10), activationAttributes[types.AttributeKeyGeneration])
	require.Equal(t, strconv.FormatUint(activated.GetRevision(), 10), activationAttributes[types.AttributeKeyRevision])
}

func TestVolumeGenerationCapFailureRollsBackActivationPruneAndEvents(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := types.SwapDirection_SWAP_DIRECTION_A_TO_B
	effectiveAt := uint64(f.ctx.BlockTime().Unix() + 1)
	pending, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&types.ExchangeUpdatePatch{
			PendingVolumeEpochSeconds:         types.NewUInt32Value(minVolumeEpochSecs * 2),
			PendingVolumeEpochEffectiveAtUnix: types.NewUInt64Value(effectiveAt),
		},
	)
	require.NoError(t, err)
	pendingSnapshot := types.CloneMessage(pending)
	futureCtx := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(2 * time.Second))
	currentStart := uint64(futureCtx.BlockTime().Unix()) / uint64(minVolumeEpochSecs) * uint64(minVolumeEpochSecs)
	expiredKey := volumeWindowKeyFromStart(
		exchange.GetId(),
		direction,
		currentStart-2*uint64(minVolumeEpochSecs),
		minVolumeEpochSecs,
		pending.GetVolumeWindowGeneration(),
	)
	require.NoError(t, f.keeper.volumeWindow.Set(futureCtx, expiredKey, "7"))
	newKey := currentVolumeKey(
		futureCtx.BlockTime(),
		exchange.GetId(),
		direction,
		pending.GetPendingVolumeEpochSeconds(),
		pending.GetVolumeWindowGeneration()+1,
	)
	eventCount := len(f.ctx.EventManager().Events())

	err = f.keeper.RecordVolumeWindow(futureCtx, exchange.GetId(), direction, sdkmath.NewInt(1001))
	require.ErrorIs(t, err, types.ErrVolumeCapExceeded)
	require.Len(t, f.ctx.EventManager().Events(), eventCount)

	unchanged, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(pendingSnapshot, unchanged))
	require.Equal(t, pendingSnapshot.GetVolumeWindowGeneration(), unchanged.GetVolumeWindowGeneration())
	require.Equal(t, pendingSnapshot.GetRevision(), unchanged.GetRevision())
	require.Equal(t, pendingSnapshot.GetPendingVolumeEpochSeconds(), unchanged.GetPendingVolumeEpochSeconds())
	require.Equal(t, pendingSnapshot.GetPendingVolumeEpochEffectiveAtUnix(), unchanged.GetPendingVolumeEpochEffectiveAtUnix())
	expiredAmount, err := f.keeper.volumeWindow.Get(f.ctx, expiredKey)
	require.NoError(t, err)
	require.Equal(t, "7", expiredAmount, "pruning must roll back with the failed record")
	_, err = f.keeper.volumeWindow.Get(f.ctx, newKey)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestVolumeGenerationChangesOnlyForAccountingIdentity(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	directions := []types.SwapDirection{
		types.SwapDirection_SWAP_DIRECTION_A_TO_B,
		types.SwapDirection_SWAP_DIRECTION_B_TO_A,
	}
	oldKeys := make(map[types.SwapDirection]volumeWindowKey, len(directions))
	for i, direction := range directions {
		oldKeys[direction] = currentVolumeKey(
			f.ctx.BlockTime(),
			exchange.GetId(),
			direction,
			exchange.GetVolumeEpochSeconds(),
			exchange.GetVolumeWindowGeneration(),
		)
		require.NoError(t, f.keeper.RecordVolumeWindow(
			f.ctx,
			exchange.GetId(),
			direction,
			sdkmath.NewInt(int64(i+1)),
		))
	}

	nextAdmin, _ := testAddress(t, f.accountCodec, 0x73)
	unrelated, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&types.ExchangeUpdatePatch{
			MaxOracleStalenessSeconds: types.NewUInt32Value(exchange.GetMaxOracleStalenessSeconds() + 1),
			FeeBpsAToB:                types.NewUInt32Value(exchange.GetFeeBpsAToB() + 1),
			VolumeCapAToB:             types.NewStringValue("999"),
			Metadata:                  map[string]string{"accounting": "unchanged"},
			NewAdminAddress:           types.NewStringValue(nextAdmin),
		},
	)
	require.NoError(t, err)
	require.Equal(t, exchange.GetVolumeWindowGeneration(), unrelated.GetVolumeWindowGeneration())
	require.Equal(t, nextAdmin, unrelated.GetAdminAddress())

	inactive, err := f.keeper.UpdateExchange(
		f.ctx,
		nextAdmin,
		unrelated.GetId(),
		unrelated.GetRevision(),
		&types.ExchangeUpdatePatch{
			Status: &types.ExchangeStatusPatch{Status: types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE},
		},
	)
	require.NoError(t, err)
	require.Equal(t, unrelated.GetVolumeWindowGeneration(), inactive.GetVolumeWindowGeneration())

	routed, err := f.keeper.UpdateExchange(
		f.ctx,
		nextAdmin,
		inactive.GetId(),
		inactive.GetRevision(),
		&types.ExchangeUpdatePatch{
			DenomA:   types.NewStringValue("uatom"),
			ChannelA: types.NewStringValue("channel-2"),
		},
	)
	require.NoError(t, err)
	require.Equal(t, inactive.GetVolumeWindowGeneration()+1, routed.GetVolumeWindowGeneration(), "one multi-field route patch must increment once")
	for _, direction := range directions {
		used, err := f.keeper.GetCurrentVolumeAmount(f.ctx, routed, direction)
		require.NoError(t, err)
		require.True(t, used.IsZero(), "route identity change must reset both directions")
		oldAmount, err := f.keeper.volumeWindow.Get(f.ctx, oldKeys[direction])
		require.NoError(t, err)
		require.NotEqual(t, "0", oldAmount)
	}

	eventCount := len(f.ctx.EventManager().Events())
	_, err = f.keeper.UpdateExchange(
		f.ctx,
		nextAdmin,
		routed.GetId(),
		routed.GetRevision(),
		&types.ExchangeUpdatePatch{
			DenomA:   types.NewStringValue(routed.GetDenomA()),
			ChannelA: types.NewStringValue(routed.GetChannelA()),
		},
	)
	require.ErrorIs(t, err, types.ErrNoOpUpdate)
	persisted, err := f.keeper.GetExchange(f.ctx, routed.GetId())
	require.NoError(t, err)
	require.Equal(t, routed.GetVolumeWindowGeneration(), persisted.GetVolumeWindowGeneration())
	require.Equal(t, routed.GetRevision(), persisted.GetRevision())
	require.Len(t, f.ctx.EventManager().Events(), eventCount)
}

func TestVolumeGenerationOverflowIsRejectedWithoutMutation(t *testing.T) {
	t.Run("immediate epoch update", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
		exhausted := cloneExchange(exchange)
		exhausted.VolumeWindowGeneration = ^uint64(0)
		require.NoError(t, f.keeper.exchanges.Set(f.ctx, exhausted.GetId(), exhausted))
		eventCount := len(f.ctx.EventManager().Events())

		_, err := f.keeper.UpdateExchange(
			f.ctx,
			f.admin,
			exhausted.GetId(),
			exhausted.GetRevision(),
			&types.ExchangeUpdatePatch{VolumeEpochSeconds: types.NewUInt32Value(minVolumeEpochSecs * 2)},
		)
		require.ErrorIs(t, err, types.ErrInvariantViolation)
		persisted, err := f.keeper.GetExchange(f.ctx, exhausted.GetId())
		require.NoError(t, err)
		require.True(t, types.EqualMessages(exhausted, persisted))
		require.Len(t, f.ctx.EventManager().Events(), eventCount)
	})

	t.Run("due pending activation", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
		exhausted := cloneExchange(exchange)
		exhausted.VolumeWindowGeneration = ^uint64(0)
		exhausted.PendingVolumeEpochSeconds = minVolumeEpochSecs * 2
		exhausted.PendingVolumeEpochEffectiveAtUnix = uint64(f.ctx.BlockTime().Unix())
		require.NoError(t, f.keeper.exchanges.Set(f.ctx, exhausted.GetId(), exhausted))
		eventCount := len(f.ctx.EventManager().Events())

		_, err := f.keeper.GetCurrentVolumeAmount(
			f.ctx,
			exhausted,
			types.SwapDirection_SWAP_DIRECTION_A_TO_B,
		)
		require.ErrorIs(t, err, types.ErrInvariantViolation)
		err = f.keeper.RecordVolumeWindow(
			f.ctx,
			exhausted.GetId(),
			types.SwapDirection_SWAP_DIRECTION_A_TO_B,
			sdkmath.OneInt(),
		)
		require.ErrorIs(t, err, types.ErrInvariantViolation)
		persisted, err := f.keeper.GetExchange(f.ctx, exhausted.GetId())
		require.NoError(t, err)
		require.True(t, types.EqualMessages(exhausted, persisted))
		require.Len(t, f.ctx.EventManager().Events(), eventCount)
	})
}

func TestVolumeWindowInvariantRejectsInvalidGeneration(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		generation func(*types.Exchange) uint64
	}{
		{
			name: "zero",
			generation: func(*types.Exchange) uint64 {
				return 0
			},
		},
		{
			name: "greater than exchange",
			generation: func(exchange *types.Exchange) uint64 {
				return exchange.GetVolumeWindowGeneration() + 1
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
			key := currentVolumeKey(
				f.ctx.BlockTime(),
				exchange.GetId(),
				types.SwapDirection_SWAP_DIRECTION_A_TO_B,
				exchange.GetVolumeEpochSeconds(),
				testCase.generation(exchange),
			)
			require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, key, "1"))
			require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
		})
	}
}

func TestVolumeGenerationKeeperGenesisRoundTripPreservesDistinctRows(t *testing.T) {
	source := setupKeeperFixture(t)
	require.NoError(t, source.keeper.RegisterAdmin(source.ctx, source.moderator, source.admin))
	exchange := registerExchange(t, source, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	current := cloneExchange(exchange)
	current.VolumeWindowGeneration = 2
	require.NoError(t, source.keeper.exchanges.Set(source.ctx, current.GetId(), current))

	epochSeconds := current.GetVolumeEpochSeconds()
	epochStart := uint64(source.ctx.BlockTime().Unix()) / uint64(epochSeconds) * uint64(epochSeconds)
	direction := types.SwapDirection_SWAP_DIRECTION_A_TO_B
	require.NoError(t, source.keeper.volumeWindow.Set(
		source.ctx,
		volumeWindowKeyFromStart(current.GetId(), direction, epochStart, epochSeconds, 1),
		"5",
	))
	require.NoError(t, source.keeper.volumeWindow.Set(
		source.ctx,
		volumeWindowKeyFromStart(current.GetId(), direction, epochStart, epochSeconds, 2),
		"7",
	))

	genesis, err := source.keeper.ExportGenesis(source.ctx)
	require.NoError(t, err)
	require.Len(t, genesis.GetVolumeWindows(), 2)

	target := setupKeeperFixture(t)
	require.NoError(t, target.keeper.ImportGenesis(target.ctx, genesis))
	require.NoError(t, target.keeper.AssertInvariants(target.ctx))
	roundTrip, err := target.keeper.ExportGenesis(target.ctx)
	require.NoError(t, err)

	amountByGeneration := make(map[uint64]string, len(roundTrip.GetVolumeWindows()))
	for _, window := range roundTrip.GetVolumeWindows() {
		require.Equal(t, current.GetId(), window.GetExchangeId())
		require.Equal(t, direction, window.GetDirection())
		require.Equal(t, epochStart, window.GetEpochStartUnix())
		require.Equal(t, epochSeconds, window.GetEpochSeconds())
		amountByGeneration[window.GetVolumeWindowGeneration()] = window.GetAmount()
	}
	require.Equal(t, map[uint64]string{1: "5", 2: "7"}, amountByGeneration)
}
