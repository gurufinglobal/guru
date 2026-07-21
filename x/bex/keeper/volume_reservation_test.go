package keeper

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

func TestVolumeReservationReleasesExactOriginalWindow(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := types.SwapDirection_SWAP_DIRECTION_A_TO_B

	reservation, err := f.keeper.ReserveVolumeWindow(f.ctx, exchange.GetId(), direction, sdkmath.NewInt(17))
	require.NoError(t, err)
	require.Equal(t, exchange.GetId(), reservation.GetExchangeId())
	require.Equal(t, direction, reservation.GetDirection())
	require.Equal(t, "17", reservation.GetAmount())
	oldKey := volumeWindowKeyFromStart(
		reservation.GetExchangeId(),
		reservation.GetDirection(),
		reservation.GetEpochStartUnix(),
		reservation.GetEpochSeconds(),
		reservation.GetVolumeWindowGeneration(),
	)
	stored, err := f.keeper.volumeWindow.Get(f.ctx, oldKey)
	require.NoError(t, err)
	require.Equal(t, "17", stored)

	updated, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&types.ExchangeUpdatePatch{VolumeEpochSeconds: types.NewUInt32Value(minVolumeEpochSecs * 2)},
	)
	require.NoError(t, err)
	require.NotEqual(t, reservation.GetVolumeWindowGeneration(), updated.GetVolumeWindowGeneration())

	require.NoError(t, f.keeper.ReleaseVolumeWindow(f.ctx, reservation))
	_, err = f.keeper.volumeWindow.Get(f.ctx, oldKey)
	require.ErrorIs(t, err, collections.ErrNotFound)
	current, err := f.keeper.GetCurrentVolumeAmount(f.ctx, updated, direction)
	require.NoError(t, err)
	require.True(t, current.IsZero())
}

func TestVolumeReservationReleaseRejectsLiveAccountingCorruption(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reservation, err := f.keeper.ReserveVolumeWindow(
		f.ctx,
		exchange.GetId(),
		types.SwapDirection_SWAP_DIRECTION_A_TO_B,
		sdkmath.NewInt(9),
	)
	require.NoError(t, err)
	key := volumeWindowKeyFromStart(
		reservation.GetExchangeId(),
		reservation.GetDirection(),
		reservation.GetEpochStartUnix(),
		reservation.GetEpochSeconds(),
		reservation.GetVolumeWindowGeneration(),
	)
	require.NoError(t, f.keeper.volumeWindow.Remove(f.ctx, key))

	err = f.keeper.ReleaseVolumeWindow(f.ctx, reservation)
	require.ErrorIs(t, err, types.ErrInvariantViolation)
}

func TestVolumeReservationReleaseAllowsAlreadyPrunedExpiredWindow(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reservation, err := f.keeper.ReserveVolumeWindow(
		f.ctx,
		exchange.GetId(),
		types.SwapDirection_SWAP_DIRECTION_A_TO_B,
		sdkmath.NewInt(5),
	)
	require.NoError(t, err)
	key := volumeWindowKeyFromStart(
		reservation.GetExchangeId(),
		reservation.GetDirection(),
		reservation.GetEpochStartUnix(),
		reservation.GetEpochSeconds(),
		reservation.GetVolumeWindowGeneration(),
	)
	require.NoError(t, f.keeper.volumeWindow.Remove(f.ctx, key))
	expiredCtx := f.ctx.WithBlockTime(time.Unix(
		int64(reservation.GetEpochStartUnix()+uint64(reservation.GetEpochSeconds())), //nolint:gosec // validated bounded timestamp.
		0,
	))
	require.NoError(t, f.keeper.ReleaseVolumeWindow(expiredCtx, reservation))
}
