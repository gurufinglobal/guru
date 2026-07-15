package keeper

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/stretchr/testify/require"
)

func TestPruneExpiredVolumeWindowsUsesExpiryOrder(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B

	// The older-starting long window is still live, while the later-starting
	// short window is expired. Start-order pruning would stop at the live row
	// and leave the expired row behind.
	now := uint64(5*minVolumeEpochSecs + 1)
	liveLong := volumeWindowKeyFromStart(
		exchange.GetId(),
		direction,
		0,
		6*minVolumeEpochSecs,
		exchange.GetVolumeWindowGeneration(),
	)
	expiredShort := volumeWindowKeyFromStart(
		exchange.GetId(),
		direction,
		4*uint64(minVolumeEpochSecs),
		minVolumeEpochSecs,
		exchange.GetVolumeWindowGeneration(),
	)
	liveStart, ok := volumeWindowEpochStart(liveLong)
	require.True(t, ok)
	expiredStart, ok := volumeWindowEpochStart(expiredShort)
	require.True(t, ok)
	require.Less(t, liveStart, expiredStart)
	require.Greater(t, liveLong.K1(), expiredShort.K1())

	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, liveLong, "1"))
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, expiredShort, "1"))
	pruneCtx := f.ctx.WithBlockTime(time.Unix(int64(now), 0))
	require.NoError(t, f.keeper.pruneExpiredVolumeWindows(pruneCtx, 1))

	_, err := f.keeper.volumeWindow.Get(pruneCtx, expiredShort)
	require.ErrorIs(t, err, collections.ErrNotFound)
	value, err := f.keeper.volumeWindow.Get(pruneCtx, liveLong)
	require.NoError(t, err)
	require.Equal(t, "1", value)
}

func TestRecordVolumeWindowLazilyPrunesOnlyForNewWindow(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B

	epochSeconds := uint64(minVolumeEpochSecs)
	now := 100*epochSeconds + 1
	recordCtx := f.ctx.WithBlockTime(time.Unix(int64(now), 0))
	for i := uint64(0); i < maxVolumePruneRowsPerPass+1; i++ {
		key := volumeWindowKeyFromStart(
			exchange.GetId(),
			direction,
			i*epochSeconds,
			minVolumeEpochSecs,
			exchange.GetVolumeWindowGeneration(),
		)
		require.NoError(t, f.keeper.volumeWindow.Set(recordCtx, key, "1"))
	}

	countExpired := func(ctx context.Context, at uint64) int {
		t.Helper()
		count := 0
		err := f.keeper.volumeWindow.Walk(ctx, nil, func(key volumeWindowKey, _ string) (bool, error) {
			if key.K1() <= at {
				count++
			}
			return false, nil
		})
		require.NoError(t, err)
		return count
	}

	// Creating a new current row prunes at most 32 expired rows globally.
	require.NoError(t, f.keeper.RecordVolumeWindow(recordCtx, exchange.GetId(), direction, sdkmath.OneInt()))
	require.Equal(t, 1, countExpired(recordCtx, now))

	currentKey := currentVolumeKey(
		recordCtx.BlockTime(),
		exchange.GetId(),
		direction,
		minVolumeEpochSecs,
		exchange.GetVolumeWindowGeneration(),
	)
	value, err := f.keeper.volumeWindow.Get(recordCtx, currentKey)
	require.NoError(t, err)
	require.Equal(t, "1", value)

	// Accumulating into an existing current row does not run lazy pruning.
	require.NoError(t, f.keeper.RecordVolumeWindow(recordCtx, exchange.GetId(), direction, sdkmath.OneInt()))
	require.Equal(t, 1, countExpired(recordCtx, now))
	value, err = f.keeper.volumeWindow.Get(recordCtx, currentKey)
	require.NoError(t, err)
	require.Equal(t, "2", value)

	// The next epoch creates another row and removes the remaining backlog.
	nextNow := now + epochSeconds
	nextCtx := recordCtx.WithBlockTime(time.Unix(int64(nextNow), 0))
	require.NoError(t, f.keeper.RecordVolumeWindow(nextCtx, exchange.GetId(), direction, sdkmath.OneInt()))
	require.Zero(t, countExpired(nextCtx, nextNow))
}
