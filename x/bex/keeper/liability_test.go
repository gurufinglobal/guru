package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestReserveIntegrationAPIsUseExactCapabilities(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reserve := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
	coin := sdk.NewInt64Coin(exchange.GetIbcDenomA(), 10)
	coins := sdk.NewCoins(coin)
	f.bankKeeper.SetBalance(f.adminAddr, coins)

	require.ErrorIs(t, f.bankKeeper.SendCoins(f.ctx, f.adminAddr, reserve, coins), types.ErrDirectReserveTransfer)
	require.NoError(t, f.keeper.ReceiveToReserve(f.ctx, exchange.GetId(), f.adminAddr, coins))
	require.Equal(t, coins, f.bankKeeper.GetAllBalances(f.ctx, reserve))

	require.ErrorIs(t, f.bankKeeper.SendCoins(f.ctx, reserve, f.recipient, coins), types.ErrDirectReserveTransfer)
	require.NoError(t, f.keeper.SendFromReserve(f.ctx, exchange.GetId(), f.recipient, coins))
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, reserve).IsZero())
	require.Equal(t, coins, f.bankKeeper.GetAllBalances(f.ctx, f.recipient))
}

func TestValidateSwapInputBindsWireDenomToConfiguredLocalVoucher(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	direction, err := f.keeper.ValidateSwapInput(f.ctx, exchange.GetId(), exchange.GetDenomA(), exchange.GetIbcDenomA())
	require.NoError(t, err)
	require.Equal(t, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, direction)

	_, err = f.keeper.ValidateSwapInput(f.ctx, exchange.GetId(), exchange.GetDenomA(), exchange.GetIbcDenomB())
	require.ErrorIs(t, err, types.ErrInvalidRoute)
}

func TestPendingLiabilityRestrictsWithdrawalRouteChangeAndDelete(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	liability := sdk.NewInt64Coin(exchange.GetIbcDenomA(), 40)
	require.NoError(t, f.keeper.AddPendingLiability(f.ctx, exchange.GetId(), liability))

	pending, err := f.keeper.GetPendingLiabilities(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(liability), pending)

	inactive, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&bexv1.ExchangeUpdatePatch{Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE}},
	)
	require.NoError(t, err)

	reserve := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
	f.bankKeeper.SetBalance(reserve, sdk.NewCoins(sdk.NewInt64Coin(exchange.GetIbcDenomA(), 100)))
	require.ErrorIs(
		t,
		f.keeper.WithdrawReserve(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin(exchange.GetIbcDenomA(), 61))),
		types.ErrInsufficientReserve,
	)
	require.NoError(t, f.keeper.WithdrawReserve(
		f.ctx,
		f.admin,
		exchange.GetId(),
		f.recipient,
		sdk.NewCoins(sdk.NewInt64Coin(exchange.GetIbcDenomA(), 60)),
	))

	_, err = f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		inactive.GetRevision(),
		&bexv1.ExchangeUpdatePatch{DenomA: wrapperspb.String("newagxn")},
	)
	require.ErrorIs(t, err, types.ErrInvalidRoute)

	f.bankKeeper.SetBalance(reserve, nil)
	require.ErrorIs(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()), types.ErrInvalidRequest)

	require.NoError(t, f.keeper.ReleasePendingLiability(f.ctx, exchange.GetId(), liability))
	pending, err = f.keeper.GetPendingLiabilities(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, pending.IsZero())
}

func TestPendingLiabilityKeeperGenesisRoundTrip(t *testing.T) {
	source := setupKeeperFixture(t)
	require.NoError(t, source.keeper.RegisterAdmin(source.ctx, source.moderator, source.admin))
	exchange := registerExchange(t, source, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	liability := sdk.NewInt64Coin(exchange.GetIbcDenomA(), 95)
	require.NoError(t, source.keeper.AddPendingLiability(source.ctx, exchange.GetId(), liability))

	genesis, err := source.keeper.ExportGenesis(source.ctx)
	require.NoError(t, err)
	require.Len(t, genesis.GetPendingLiabilities(), 1)

	target := setupKeeperFixture(t)
	require.NoError(t, target.keeper.ImportGenesis(target.ctx, genesis))
	pending, err := target.keeper.GetPendingLiabilities(target.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(liability), pending)
}
