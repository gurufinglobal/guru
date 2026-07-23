package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	bexv1 "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestReserveMessagesEnforceBankSendPolicies(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	reserveBytes, err := f.accountCodec.StringToBytes(exchange.GetReserveAddress())
	require.NoError(t, err)
	reserve := sdk.AccAddress(reserveBytes)
	amount := sdk.NewCoins(sdk.NewInt64Coin("agxn", 10))

	f.bankKeeper.SetSendEnabled("agxn", false)
	require.ErrorIs(t, f.keeper.DepositReserve(f.ctx, f.admin, exchange.GetId(), amount), banktypes.ErrSendDisabled)
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, reserve).IsZero())
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1_000_000)), f.bankKeeper.GetAllBalances(f.ctx, f.adminAddr))

	f.bankKeeper.SetSendEnabled("agxn", true)
	require.NoError(t, f.keeper.DepositReserve(f.ctx, f.admin, exchange.GetId(), amount))

	f.bankKeeper.SetSendEnabled("agxn", false)
	require.ErrorIs(t, f.keeper.WithdrawReserve(f.ctx, f.admin, exchange.GetId(), f.recipient, amount), banktypes.ErrSendDisabled)
	require.Equal(t, amount, f.bankKeeper.GetAllBalances(f.ctx, reserve))
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, f.recipient).IsZero())

	f.bankKeeper.SetSendEnabled("agxn", true)
	f.bankKeeper.SetBlockedAddr(f.recipient, true)
	require.ErrorIs(t, f.keeper.WithdrawReserve(f.ctx, f.admin, exchange.GetId(), f.recipient, amount), sdkerrors.ErrUnauthorized)
	require.Equal(t, amount, f.bankKeeper.GetAllBalances(f.ctx, reserve))
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, f.recipient).IsZero())

	f.bankKeeper.SetBlockedAddr(f.recipient, false)
	require.NoError(t, f.keeper.WithdrawReserve(f.ctx, f.admin, exchange.GetId(), f.recipient, amount))
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, reserve).IsZero())
	require.Equal(t, amount, f.bankKeeper.GetAllBalances(f.ctx, f.recipient))
}
