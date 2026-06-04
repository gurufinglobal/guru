package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestNewKeeperInitializesCollections(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	run := func() {
		_, _ = f.keeper.params.Has(f.ctx)
		_, _ = f.keeper.baseAddress.Has(f.ctx)
		_, _ = f.keeper.moderatorAddress.Has(f.ctx)
		_, _ = f.keeper.separationRatio.Has(f.ctx)
	}
	require.NotPanics(t, run)
}

func TestKeeperLogger(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	logger := f.keeper.Logger(f.ctx)
	require.NotNil(t, logger)
	require.NotPanics(t, func() { logger.Info("keeper logger test") })
}

func TestKeeperBaseAndModeratorAddressGetSetUpdate(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	baseAddress := testAddress(t, f.keeper.accountCodec, 0x11)
	moderatorAddress := testAddress(t, f.keeper.accountCodec, 0x12)

	require.NoError(t, f.keeper.SetBaseAddress(f.ctx, baseAddress))
	require.NoError(t, f.keeper.SetModeratorAddress(f.ctx, moderatorAddress))

	gotBaseAddress, err := f.keeper.GetBaseAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, baseAddress, gotBaseAddress)

	gotModeratorAddress, err := f.keeper.GetModeratorAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, moderatorAddress, gotModeratorAddress)

	updatedBaseAddress := testAddress(t, f.keeper.accountCodec, 0x13)
	updatedModeratorAddress := testAddress(t, f.keeper.accountCodec, 0x14)

	require.NoError(t, f.keeper.UpdateBaseAddress(f.ctx, updatedBaseAddress))
	require.NoError(t, f.keeper.UpdateModeratorAddress(f.ctx, updatedModeratorAddress))

	gotBaseAddress, err = f.keeper.GetBaseAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, updatedBaseAddress, gotBaseAddress)

	gotModeratorAddress, err = f.keeper.GetModeratorAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, updatedModeratorAddress, gotModeratorAddress)
}

func TestKeeperBaseAndModeratorAddressValidation(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	require.Error(t, f.keeper.SetBaseAddress(f.ctx, "invalid"))
	require.Error(t, f.keeper.SetBaseAddress(f.ctx, ""))
	require.Error(t, f.keeper.SetBaseAddress(f.ctx, f.authority))
	require.Error(t, f.keeper.SetBaseAddress(f.ctx, testHexAddress(0x01)))
	require.Error(t, f.keeper.SetModeratorAddress(f.ctx, "invalid"))
	require.Error(t, f.keeper.SetModeratorAddress(f.ctx, ""))
	require.Error(t, f.keeper.SetModeratorAddress(f.ctx, f.authority))
	require.Error(t, f.keeper.SetModeratorAddress(f.ctx, testHexAddress(0x01)))
}

func TestKeeperBaseAddressRejectsBlockedAddress(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)
	blockedAddress := testAddress(t, f.keeper.accountCodec, 0x15)
	blockedAddressBytes, err := f.keeper.accountCodec.StringToBytes(blockedAddress)
	require.NoError(t, err)
	f.bankKeeper.SetBlockedAddr(sdk.AccAddress(blockedAddressBytes), true)

	require.Error(t, f.keeper.SetBaseAddress(f.ctx, blockedAddress))
	require.NoError(t, f.keeper.SetModeratorAddress(f.ctx, blockedAddress))
}
