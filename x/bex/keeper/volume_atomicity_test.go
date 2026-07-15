package keeper

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRecordVolumeWindowIsAtomic(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B

	effectiveAt := uint64(f.ctx.BlockTime().Add(time.Second).Unix())
	pending, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 2),
		PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(effectiveAt),
	})
	require.NoError(t, err)

	futureCtx := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(2 * time.Second))
	expiredStart := uint64(futureCtx.BlockTime().Unix()) - 2*uint64(minVolumeEpochSecs)
	expiredKey := volumeWindowKeyFromStart(
		exchange.GetId(),
		direction,
		expiredStart,
		minVolumeEpochSecs,
		pending.GetVolumeWindowGeneration(),
	)
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, expiredKey, "7"))
	newKey := currentVolumeKey(
		futureCtx.BlockTime(),
		exchange.GetId(),
		direction,
		minVolumeEpochSecs*2,
		pending.GetVolumeWindowGeneration()+1,
	)
	eventCount := len(f.ctx.EventManager().Events())

	err = f.keeper.RecordVolumeWindow(futureCtx, exchange.GetId(), direction, sdkmath.NewInt(1001))
	require.ErrorIs(t, err, types.ErrVolumeCapExceeded)
	require.Len(t, f.ctx.EventManager().Events(), eventCount)

	afterFailure, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, proto.Equal(pending, afterFailure))
	value, err := f.keeper.volumeWindow.Get(f.ctx, expiredKey)
	require.NoError(t, err)
	require.Equal(t, "7", value)
	_, err = f.keeper.volumeWindow.Get(f.ctx, newKey)
	require.ErrorIs(t, err, collections.ErrNotFound)

	require.NoError(t, f.keeper.RecordVolumeWindow(futureCtx, exchange.GetId(), direction, sdkmath.NewInt(5)))
	afterSuccess, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, minVolumeEpochSecs*2, afterSuccess.GetVolumeEpochSeconds())
	require.Zero(t, afterSuccess.GetPendingVolumeEpochSeconds())
	require.Zero(t, afterSuccess.GetPendingVolumeEpochEffectiveAtUnix())
	require.Equal(t, pending.GetRevision()+1, afterSuccess.GetRevision())
	require.Equal(t, pending.GetVolumeWindowGeneration()+1, afterSuccess.GetVolumeWindowGeneration())
	_, err = f.keeper.volumeWindow.Get(f.ctx, expiredKey)
	require.ErrorIs(t, err, collections.ErrNotFound)
	value, err = f.keeper.volumeWindow.Get(f.ctx, newKey)
	require.NoError(t, err)
	require.Equal(t, "5", value)
	requireEventTypes(t, f.ctx, types.EventTypeVolumeRecorded)
}

func TestRecordVolumeWindowRejectsInactiveExchangeWithoutMutation(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	effectiveAt := uint64(f.ctx.BlockTime().Unix())
	pending, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 2),
		PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(effectiveAt),
	})
	require.NoError(t, err)
	eventCount := len(f.ctx.EventManager().Events())

	err = f.keeper.RecordVolumeWindow(
		f.ctx,
		exchange.GetId(),
		bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
		sdkmath.OneInt(),
	)
	require.ErrorIs(t, err, types.ErrInvalidRoute)
	require.Len(t, f.ctx.EventManager().Events(), eventCount)

	persisted, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, proto.Equal(pending, persisted))
	key := currentVolumeKey(
		f.ctx.BlockTime(),
		exchange.GetId(),
		bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
		pending.GetPendingVolumeEpochSeconds(),
		pending.GetVolumeWindowGeneration()+1,
	)
	_, err = f.keeper.volumeWindow.Get(f.ctx, key)
	require.ErrorIs(t, err, collections.ErrNotFound)
}
