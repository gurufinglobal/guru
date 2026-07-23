package keeper

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestFeeCustodyLifecycleAndSolvency(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	first := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	second := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)

	collectFee(t, f, first.GetId(), sdk.NewInt64Coin("agxn", 7))
	collectFee(t, f, second.GetId(), sdk.NewInt64Coin("agxn", 5))
	collectFee(t, f, second.GetId(), sdk.NewInt64Coin("gxusd", 3))
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("agxn", 12),
		sdk.NewInt64Coin("gxusd", 3),
	), f.bankKeeper.GetAllBalances(f.ctx, moduleAddr))

	require.ErrorIs(t, f.keeper.LockExchangeFee(f.ctx, first.GetId(), sdk.NewInt64Coin("agxn", 8)), types.ErrInsufficientAvailableFees)
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, first.GetId(), sdk.NewInt64Coin("agxn", 2)))
	require.ErrorIs(t, f.keeper.ReleaseExchangeFee(f.ctx, first.GetId(), sdk.NewInt64Coin("agxn", 3)), types.ErrInsufficientLockedFees)
	require.NoError(t, f.keeper.ReleaseExchangeFee(f.ctx, first.GetId(), sdk.NewInt64Coin("agxn", 1)))
	require.NoError(t, f.keeper.RefundLockedFee(f.ctx, first.GetId(), sdk.NewInt64Coin("agxn", 1)))
	require.Equal(t, int64(1), f.bankKeeper.GetBalance(f.ctx, feeReserveAddress(t, f, first), "agxn").Amount.Int64())

	require.NoError(t, f.keeper.WithdrawFees(
		f.ctx,
		f.admin,
		second.GetId(),
		f.recipient,
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 2), sdk.NewInt64Coin("gxusd", 3)),
	))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 9)), f.bankKeeper.GetAllBalances(f.ctx, moduleAddr))
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("agxn", 2),
		sdk.NewInt64Coin("gxusd", 3),
	), f.bankKeeper.GetAllBalances(f.ctx, f.recipient))
	require.NoError(t, f.keeper.AssertFeeSolvency(f.ctx))
	require.NoError(t, f.keeper.AssertInvariants(f.ctx))
}

func TestCollectFeeMovesCustodyAndRollsBackOnFailure(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reserveAddr := feeReserveAddress(t, f, exchange)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 5)))

	require.NoError(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 3)))
	require.Equal(t, int64(2), f.bankKeeper.GetBalance(f.ctx, reserveAddr, "agxn").Amount.Int64())
	require.Equal(t, int64(3), f.bankKeeper.GetBalance(f.ctx, moduleAddr, "agxn").Amount.Int64())
	collected, err := f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, int64(3), collected.AmountOf("agxn").Int64())

	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 3)), types.ErrInsufficientReserve)
	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 0)), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("unsupported", 1)), types.ErrInvalidRoute)
	collected, err = f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, int64(3), collected.AmountOf("agxn").Int64())
	require.Equal(t, int64(2), f.bankKeeper.GetBalance(f.ctx, reserveAddr, "agxn").Amount.Int64())

	faultErr := errors.New("collected ledger write failed")
	faulty := NewKeeper(
		faultStoreService{base: f.storeService, fault: &storeFault{op: "set", prefix: 0x06, err: faultErr}},
		f.codec,
		f.accountCodec,
		f.accountKeeper,
		f.bankKeeper,
		f.oracleKeeper,
		f.constitutionKeeper,
		f.channelKeeper,
	)
	require.ErrorIs(t, faulty.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.Equal(t, int64(2), f.bankKeeper.GetBalance(f.ctx, reserveAddr, "agxn").Amount.Int64())

	bankErr := errors.New("bank send failed")
	f.bankKeeper.restrictions = append(f.bankKeeper.restrictions, func(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) (sdk.AccAddress, error) {
		return nil, bankErr
	})
	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), bankErr)
	collected, err = f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, int64(3), collected.AmountOf("agxn").Int64())
	require.Equal(t, int64(2), f.bankKeeper.GetBalance(f.ctx, reserveAddr, "agxn").Amount.Int64())
	require.Equal(t, int64(3), f.bankKeeper.GetBalance(f.ctx, moduleAddr, "agxn").Amount.Int64())
}

func TestFeeOperationsRejectUnconfiguredDenoms(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	collectFee(t, f, exchange.GetId(), sdk.NewInt64Coin("agxn", 3))
	unsupported := sdk.NewInt64Coin("unsupported", 1)

	require.ErrorIs(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), unsupported), types.ErrInvalidRoute)
	require.ErrorIs(t, f.keeper.ReleaseExchangeFee(f.ctx, exchange.GetId(), unsupported), types.ErrInvalidRoute)
	require.ErrorIs(t, f.keeper.RefundLockedFee(f.ctx, exchange.GetId(), unsupported), types.ErrInvalidRoute)
	require.ErrorIs(t, f.keeper.WithdrawFees(
		f.ctx,
		f.admin,
		exchange.GetId(),
		f.recipient,
		sdk.NewCoins(unsupported),
	), types.ErrInvalidRoute)
}

func TestWithdrawFeesRespectsSendEnabledPolicy(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	collectFee(t, f, exchange.GetId(), sdk.NewInt64Coin("agxn", 3))
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	amount := sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))

	f.bankKeeper.SetSendEnabled("agxn", false)
	require.ErrorIs(t, f.keeper.WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, amount), banktypes.ErrSendDisabled)
	collected, err := f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, int64(3), collected.AmountOf("agxn").Int64())
	require.Equal(t, int64(3), f.bankKeeper.GetBalance(f.ctx, moduleAddr, "agxn").Amount.Int64())
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, f.recipient).IsZero())

	f.bankKeeper.SetSendEnabled("agxn", true)
	require.NoError(t, f.keeper.WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, amount))
}

func TestStateTransitionPreservesOuterContextValuesThroughFeeFlow(t *testing.T) {
	type contextKey struct{}
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reserveAddr := feeReserveAddress(t, f, exchange)
	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))

	const expected = "outer-capability"
	f.bankKeeper.restrictions = append(f.bankKeeper.restrictions, func(
		ctx context.Context,
		_ sdk.AccAddress,
		to sdk.AccAddress,
		_ sdk.Coins,
	) (sdk.AccAddress, error) {
		if ctx.Value(contextKey{}) != expected {
			return nil, errors.New("outer context value was lost")
		}
		return to, nil
	})
	wrapped := context.WithValue(f.ctx, contextKey{}, expected)
	require.NoError(t, f.keeper.CollectFee(wrapped, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)))
}

func TestCollectAndLockRequireActiveExchange(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	inactive := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	reserveAddr := feeReserveAddress(t, f, inactive)
	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)))

	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, inactive.GetId(), sdk.NewInt64Coin("agxn", 1)), types.ErrInvalidRoute)
	require.ErrorIs(t, f.keeper.LockExchangeFee(f.ctx, inactive.GetId(), sdk.NewInt64Coin("agxn", 1)), types.ErrInvalidRoute)
	require.Equal(t, int64(2), f.bankKeeper.GetBalance(f.ctx, reserveAddr, "agxn").Amount.Int64())

	f.bankKeeper.SetBalance(reserveAddr, sdk.Coins{})
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, inactive.GetId()))
	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, inactive.GetId(), sdk.NewInt64Coin("agxn", 1)), types.ErrExchangeDeleted)
	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, 999, sdk.NewInt64Coin("agxn", 1)), types.ErrExchangeNotFound)
}

func TestRefundLockedFeeReturnsCustodyAndRejectsAggregateOverConsumption(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reserveAddr := feeReserveAddress(t, f, exchange)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	collectFee(t, f, exchange.GetId(), sdk.NewInt64Coin("agxn", 5))
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 3)))

	require.NoError(t, f.keeper.RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 2)))
	collected, err := f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	locked, err := f.keeper.GetLockedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, int64(3), collected.AmountOf("agxn").Int64())
	require.Equal(t, int64(1), locked.AmountOf("agxn").Int64())
	require.Equal(t, int64(3), f.bankKeeper.GetBalance(f.ctx, moduleAddr, "agxn").Amount.Int64())
	require.Equal(t, int64(2), f.bankKeeper.GetBalance(f.ctx, reserveAddr, "agxn").Amount.Int64())

	require.ErrorIs(t, f.keeper.RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 2)), types.ErrInsufficientLockedFees)
	require.Equal(t, int64(3), f.bankKeeper.GetBalance(f.ctx, moduleAddr, "agxn").Amount.Int64())
	require.Equal(t, int64(2), f.bankKeeper.GetBalance(f.ctx, reserveAddr, "agxn").Amount.Int64())
}

func TestRefundLockedFeeRollsBackLedgersWhenBankSendFails(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	collectFee(t, f, exchange.GetId(), sdk.NewInt64Coin("agxn", 3))
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 2)))

	bankErr := errors.New("bank send failed")
	f.bankKeeper.restrictions = append(f.bankKeeper.restrictions, func(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) (sdk.AccAddress, error) {
		return nil, bankErr
	})
	require.ErrorIs(t, f.keeper.RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), bankErr)
	collected, err := f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	locked, err := f.keeper.GetLockedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, int64(3), collected.AmountOf("agxn").Int64())
	require.Equal(t, int64(2), locked.AmountOf("agxn").Int64())
}

func TestFeeOutflowCapabilityAndFullAudit(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	first := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	second := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	collectFee(t, f, first.GetId(), sdk.NewInt64Coin("agxn", 3))
	collectFee(t, f, second.GetId(), sdk.NewInt64Coin("agxn", 4))
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)

	direct := sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))
	require.ErrorIs(t, f.bankKeeper.SendCoinsFromModuleToAccount(f.ctx, types.ModuleName, f.recipient, direct), types.ErrInvariantViolation)
	f.bankKeeper.SetBalance(moduleAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))
	require.ErrorIs(t, f.bankKeeper.SendCoinsFromModuleToAccount(f.ctx, types.ModuleName, f.recipient, direct), types.ErrInvariantViolation)

	other := sdk.AccAddress(bytesOf(0x31))
	allowedCtx := f.keeper.withFeeOutflowAllowance(f.ctx, f.recipient, direct)
	require.ErrorIs(t, f.bankKeeper.SendCoinsFromModuleToAccount(allowedCtx, types.ModuleName, other, direct), types.ErrInvariantViolation)
	require.ErrorIs(t, f.bankKeeper.SendCoinsFromModuleToAccount(
		allowedCtx,
		types.ModuleName,
		f.recipient,
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)),
	), types.ErrInvariantViolation)

	require.NoError(t, f.keeper.AssertFeeSolvency(f.ctx))
	f.bankKeeper.SetBalance(moduleAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 6)))
	require.ErrorIs(t, f.keeper.AssertFeeSolvency(f.ctx), types.ErrInvariantViolation)
}

func TestValidateEVMSetBalanceProtectsCustodyAddresses(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	reserveAddr := feeReserveAddress(t, f, exchange)
	normalAddr := sdk.AccAddress(bytesOf(0x41))
	f.bankKeeper.SetBalance(moduleAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 3)))
	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))

	require.ErrorIs(t, f.keeper.ValidateEVMSetBalance(f.ctx, moduleAddr, sdk.NewInt64Coin("agxn", 2)), types.ErrInvariantViolation)
	require.NoError(t, f.keeper.ValidateEVMSetBalance(f.ctx, moduleAddr, sdk.NewInt64Coin("agxn", 3)))
	require.ErrorIs(t, f.keeper.ValidateEVMSetBalance(f.ctx, moduleAddr, sdk.NewInt64Coin("agxn", 4)), types.ErrInvariantViolation)
	require.ErrorIs(t, f.keeper.ValidateEVMSetBalance(f.ctx, reserveAddr, sdk.NewInt64Coin("agxn", 0)), types.ErrDirectReserveTransfer)
	require.NoError(t, f.keeper.ValidateEVMSetBalance(f.ctx, reserveAddr, sdk.NewInt64Coin("agxn", 1)))
	require.NoError(t, f.keeper.ValidateEVMSetBalance(f.ctx, normalAddr, sdk.NewInt64Coin("agxn", 9)))

	nilBank := f.keeper
	nilBank.bankKeeper = nil
	require.ErrorIs(t, nilBank.ValidateEVMSetBalance(f.ctx, normalAddr, sdk.NewInt64Coin("agxn", 1)), types.ErrInvariantViolation)

	faultErr := errors.New("reserve index lookup failed")
	faulty := NewKeeper(
		faultStoreService{base: f.storeService, fault: &storeFault{op: "get", prefix: 0x04, err: faultErr}},
		f.codec,
		f.accountCodec,
		f.accountKeeper,
		f.bankKeeper,
		f.oracleKeeper,
		f.constitutionKeeper,
		f.channelKeeper,
	)
	require.ErrorIs(t, faulty.ValidateEVMSetBalance(f.ctx, normalAddr, sdk.NewInt64Coin("agxn", 1)), faultErr)
}

func feeReserveAddress(t *testing.T, f keeperTestFixture, exchange *types.Exchange) sdk.AccAddress {
	t.Helper()
	reserveBytes, err := f.accountCodec.StringToBytes(exchange.GetReserveAddress())
	require.NoError(t, err)
	return sdk.AccAddress(reserveBytes)
}
