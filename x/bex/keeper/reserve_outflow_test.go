package keeper

import (
	"bytes"
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

type reserveOutflowOuterContextKey struct{}

func TestReserveOutflowCapabilityRequiresExactScope(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	first := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	second := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	firstReserve := f.keeper.GetReserveAddress(f.ctx, first.GetId())
	secondReserve := f.keeper.GetReserveAddress(f.ctx, second.GetId())
	amount := sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))
	f.bankKeeper.SetBalance(firstReserve, sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))

	require.ErrorIs(
		t,
		f.bankKeeper.SendCoins(f.ctx, firstReserve, f.recipient, amount),
		types.ErrDirectReserveTransfer,
	)
	receiveOnlyCtx := f.keeper.withReserveReceiveAllowance(f.ctx, first.GetId())
	require.ErrorIs(
		t,
		f.bankKeeper.SendCoins(receiveOnlyCtx, firstReserve, f.recipient, amount),
		types.ErrDirectReserveTransfer,
	)
	feeOnlyCtx := f.keeper.withFeeOutflowAllowance(f.ctx, f.recipient, amount)
	require.ErrorIs(
		t,
		f.bankKeeper.SendCoins(feeOnlyCtx, firstReserve, f.recipient, amount),
		types.ErrDirectReserveTransfer,
	)

	tests := []struct {
		name       string
		exchangeID uint64
		recipient  sdk.AccAddress
		amount     sdk.Coins
	}{
		{
			name:       "exchange",
			exchangeID: second.GetId(),
			recipient:  f.recipient,
			amount:     amount,
		},
		{
			name:       "recipient",
			exchangeID: first.GetId(),
			recipient:  sdk.AccAddress(bytes.Repeat([]byte{0x71}, 20)),
			amount:     amount,
		},
		{
			name:       "coins",
			exchangeID: first.GetId(),
			recipient:  f.recipient,
			amount:     sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)),
		},
	}
	for _, tc := range tests {
		t.Run("rejects wrong "+tc.name, func(t *testing.T) {
			allowedCtx := f.keeper.withReserveOutflowAllowance(
				f.ctx,
				tc.exchangeID,
				tc.recipient,
				tc.amount,
			)
			require.ErrorIs(
				t,
				f.bankKeeper.SendCoins(allowedCtx, firstReserve, f.recipient, amount),
				types.ErrDirectReserveTransfer,
			)
		})
	}

	crossReserveCtx := f.keeper.withReserveOutflowAllowance(
		f.ctx,
		first.GetId(),
		secondReserve,
		amount,
	)
	require.ErrorIs(
		t,
		f.bankKeeper.SendCoins(crossReserveCtx, firstReserve, secondReserve, amount),
		types.ErrDirectReserveTransfer,
		"a reserve outflow allowance must not bypass the destination reserve inflow restriction",
	)

	outerKey := reserveOutflowOuterContextKey{}
	wrapped := context.WithValue(f.ctx, outerKey, "preserved")
	mutableRecipient := append(sdk.AccAddress(nil), f.recipient...)
	mutableAmount := append(sdk.Coins(nil), amount...)
	allowedCtx := f.keeper.withReserveOutflowAllowance(
		wrapped,
		first.GetId(),
		mutableRecipient,
		mutableAmount,
	)
	mutableRecipient[0] ^= 0xff
	mutableAmount[0] = sdk.NewInt64Coin("agxn", 2)
	require.Equal(t, "preserved", sdk.UnwrapSDKContext(allowedCtx).Value(outerKey))
	require.NoError(t, f.bankKeeper.SendCoins(allowedCtx, firstReserve, f.recipient, amount))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 9)), f.bankKeeper.GetAllBalances(f.ctx, firstReserve))
	require.Equal(t, amount, f.bankKeeper.GetAllBalances(f.ctx, f.recipient))
}

func TestReserveOperationsInstallOutflowCapability(t *testing.T) {
	t.Run("withdraw reserve", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
		reserve := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
		deposited := sdk.NewCoins(sdk.NewInt64Coin("agxn", 5))
		withdrawn := sdk.NewCoins(sdk.NewInt64Coin("agxn", 2))
		require.NoError(t, f.keeper.DepositReserve(f.ctx, f.admin, exchange.GetId(), deposited))

		require.ErrorIs(
			t,
			f.bankKeeper.SendCoins(f.ctx, reserve, f.recipient, withdrawn),
			types.ErrDirectReserveTransfer,
		)
		require.NoError(t, f.keeper.WithdrawReserve(f.ctx, f.admin, exchange.GetId(), f.recipient, withdrawn))
		require.Equal(t, deposited.Sub(withdrawn...), f.bankKeeper.GetAllBalances(f.ctx, reserve))
		require.Equal(t, withdrawn, f.bankKeeper.GetAllBalances(f.ctx, f.recipient))
		requireSingleReserveAddressAttribute(t, f.ctx, types.EventTypeReserveWithdrawn)
	})

	t.Run("collect fee", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
		reserve := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
		moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
		deposited := sdk.NewCoins(sdk.NewInt64Coin("agxn", 5))
		fee := sdk.NewInt64Coin("agxn", 2)
		coins := sdk.NewCoins(fee)
		require.NoError(t, f.keeper.DepositReserve(f.ctx, f.admin, exchange.GetId(), deposited))

		require.ErrorIs(
			t,
			f.bankKeeper.SendCoinsFromAccountToModule(f.ctx, reserve, types.ModuleName, coins),
			types.ErrDirectReserveTransfer,
		)
		require.NoError(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), fee))
		require.Equal(t, deposited.Sub(coins...), f.bankKeeper.GetAllBalances(f.ctx, reserve))
		require.Equal(t, coins, f.bankKeeper.GetAllBalances(f.ctx, moduleAddr))
		collected, err := f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
		require.NoError(t, err)
		require.Equal(t, coins, collected)
		requireSingleReserveAddressAttribute(t, f.ctx, types.EventTypeFeesCollected)

		require.ErrorIs(
			t,
			f.bankKeeper.SendCoinsFromModuleToAccount(f.ctx, types.ModuleName, f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))),
			types.ErrInvariantViolation,
			"reserve outflow support must not weaken BEX module fee custody",
		)
		require.NoError(t, f.keeper.WithdrawFees(
			f.ctx,
			f.admin,
			exchange.GetId(),
			f.recipient,
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		))
	})
}

func requireSingleReserveAddressAttribute(t *testing.T, ctx sdk.Context, eventType string) {
	t.Helper()
	found := false
	for _, event := range ctx.EventManager().Events() {
		if event.Type != eventType {
			continue
		}
		found = true
		count := 0
		for _, attribute := range event.Attributes {
			if attribute.Key == types.AttributeKeyReserveAddress {
				count++
			}
		}
		require.Equal(t, 1, count, "event %s must contain reserve_address exactly once", eventType)
	}
	require.True(t, found, "event %s was not emitted", eventType)
}
