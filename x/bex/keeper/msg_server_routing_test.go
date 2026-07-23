package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestMsgServerRejectsNilRequestsAndMapsExchangeFields(t *testing.T) {
	f := setupKeeperFixture(t)
	server := NewMsgServer(&f.keeper)

	for _, call := range []func() error{
		func() error { _, err := server.RegisterAdmin(f.ctx, nil); return err },
		func() error { _, err := server.UpdateAdmin(f.ctx, nil); return err },
		func() error { _, err := server.RemoveAdmin(f.ctx, nil); return err },
		func() error { _, err := server.RegisterExchange(f.ctx, nil); return err },
		func() error { _, err := server.UpdateExchange(f.ctx, nil); return err },
		func() error { _, err := server.DeleteExchange(f.ctx, nil); return err },
		func() error { _, err := server.AddReserveDepositor(f.ctx, nil); return err },
		func() error { _, err := server.RemoveReserveDepositor(f.ctx, nil); return err },
		func() error { _, err := server.DepositReserve(f.ctx, nil); return err },
		func() error { _, err := server.WithdrawReserve(f.ctx, nil); return err },
		func() error { _, err := server.WithdrawFees(f.ctx, nil); return err },
	} {
		require.ErrorIs(t, call(), types.ErrInvalidRequest)
	}

	_, err := server.RegisterAdmin(f.ctx, &types.MsgRegisterAdmin{
		Moderator:    f.moderator,
		AdminAddress: f.admin,
	})
	require.NoError(t, err)
	registered, err := server.RegisterExchange(
		f.ctx,
		validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE),
	)
	require.NoError(t, err)
	require.NotZero(t, registered.GetExchangeId())
	require.NotEmpty(t, registered.GetReserveAddress())

	updated, err := server.UpdateExchange(f.ctx, &types.MsgUpdateExchange{
		AdminAddress:     f.admin,
		ExchangeId:       registered.GetExchangeId(),
		ExpectedRevision: 1,
		Patch:            &types.ExchangeUpdatePatch{FeeBpsAToB: types.NewUInt32Value(7)},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), updated.GetRevision())

	_, err = server.DepositReserve(f.ctx, &types.MsgDepositReserve{
		Sender:     f.admin,
		ExchangeId: registered.GetExchangeId(),
		Amount:     sdk.Coins{{Denom: "agxn", Amount: sdkmath.Int{}}},
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = server.WithdrawReserve(f.ctx, &types.MsgWithdrawReserve{
		AdminAddress: f.admin,
		ExchangeId:   registered.GetExchangeId(),
		Amount:       sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		Recipient:    "bad",
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

func TestMsgServerRoutesUpdateAdmin(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	server := NewMsgServer(&f.keeper)

	wrongModerator, _ := testAddress(t, f.accountCodec, 0x60)
	newBEXAdmin, _ := testAddress(t, f.accountCodec, 0x61)
	_, err := server.UpdateAdmin(f.ctx, &types.MsgUpdateAdmin{
		Moderator:       wrongModerator,
		OldAdminAddress: f.admin,
		NewAdminAddress: newBEXAdmin,
	})
	require.ErrorIs(t, err, types.ErrInvalidModerator)

	updateResponse, err := server.UpdateAdmin(f.ctx, &types.MsgUpdateAdmin{
		Moderator:       f.moderator,
		OldAdminAddress: f.admin,
		NewAdminAddress: newBEXAdmin,
	})
	require.NoError(t, err)
	require.NotNil(t, updateResponse)
	registered, err := f.keeper.IsAdmin(f.ctx, f.admin)
	require.NoError(t, err)
	require.False(t, registered)
	registered, err = f.keeper.IsAdmin(f.ctx, newBEXAdmin)
	require.NoError(t, err)
	require.True(t, registered)
}
