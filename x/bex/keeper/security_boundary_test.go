package keeper

import (
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestQuoteSwapRejectsFutureOracleTimestamp(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	now := f.ctx.BlockTime()
	require.NoError(t, validateOracleFreshness(now, now.Unix(), exchange.GetMaxOracleStalenessSeconds()))
	require.NoError(t, validateOracleFreshness(now, now.Unix()-int64(exchange.GetMaxOracleStalenessSeconds()), exchange.GetMaxOracleStalenessSeconds()))
	require.ErrorIs(t, validateOracleFreshness(now, 0, exchange.GetMaxOracleStalenessSeconds()), types.ErrStaleOracleRate)
	require.ErrorIs(t, validateOracleFreshness(now, now.Unix()+1, exchange.GetMaxOracleStalenessSeconds()), types.ErrStaleOracleRate)
	require.ErrorIs(
		t,
		validateOracleFreshness(now, now.Unix()-int64(exchange.GetMaxOracleStalenessSeconds())-1, exchange.GetMaxOracleStalenessSeconds()),
		types.ErrStaleOracleRate,
	)

	f.oracleKeeper.SetValue(exchange.GetOracleSymbolAToB(), "1", now.Unix()+1)
	var quoteErr error
	require.NotPanics(t, func() {
		_, quoteErr = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
			ExchangeId: exchange.GetId(),
			InputDenom: exchange.GetDenomA(),
			AmountIn:   "2",
		})
	})
	require.ErrorIs(t, quoteErr, types.ErrStaleOracleRate)
}

func TestVolumeAccountingUint256BoundaryReturnsError(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	unlimited := cloneExchange(exchange)
	unlimited.FeeBpsAToB = 0
	unlimited.LimitAToB = "0"
	unlimited.VolumeCapAToB = "0"
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, unlimited.GetId(), unlimited))
	f.oracleKeeper.SetValue(unlimited.GetOracleSymbolAToB(), "1", f.ctx.BlockTime().Unix())

	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B
	var nilAmountErr error
	require.NotPanics(t, func() {
		nilAmountErr = f.keeper.RecordVolumeWindow(f.ctx, unlimited.GetId(), direction, sdkmath.Int{})
	})
	require.ErrorIs(t, nilAmountErr, types.ErrInvalidRequest)
	key := currentVolumeKey(
		f.ctx.BlockTime(),
		unlimited.GetId(),
		direction,
		unlimited.GetVolumeEpochSeconds(),
		unlimited.GetVolumeWindowGeneration(),
	)
	maxMinusOne := maxUint256Int.Sub(sdkmath.OneInt())
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, key, maxMinusOne.String()))

	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, unlimited.GetId(), direction, sdkmath.OneInt()))
	stored, err := f.keeper.volumeWindow.Get(f.ctx, key)
	require.NoError(t, err)
	require.Equal(t, maxUint256String, stored)

	var quoteErr error
	require.NotPanics(t, func() {
		_, quoteErr = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
			ExchangeId: unlimited.GetId(),
			InputDenom: unlimited.GetDenomA(),
			AmountIn:   "1",
		})
	})
	require.ErrorIs(t, quoteErr, types.ErrVolumeCapExceeded)

	var recordErr error
	require.NotPanics(t, func() {
		recordErr = f.keeper.RecordVolumeWindow(f.ctx, unlimited.GetId(), direction, sdkmath.OneInt())
	})
	require.ErrorIs(t, recordErr, types.ErrVolumeCapExceeded)
	stored, err = f.keeper.volumeWindow.Get(f.ctx, key)
	require.NoError(t, err)
	require.Equal(t, maxUint256String, stored)
}

func TestFeeLedgersUint256BoundaryReturnError(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	denom := "agxn"
	one := sdk.NewInt64Coin(denom, 1)
	maxMinusOne := maxUint256Int.Sub(sdkmath.OneInt())
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	reserveAddr := feeReserveAddress(t, f, exchange)
	f.bankKeeper.SetBalance(moduleAddr, sdk.NewCoins(sdk.NewCoin(denom, maxMinusOne)))
	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(one))
	require.NoError(t, f.keeper.collectedFees.Set(
		f.ctx,
		exchange.GetId(),
		coinsToLedger(sdk.NewCoins(sdk.NewCoin(denom, maxMinusOne))),
	))

	require.NoError(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), one))
	collected, err := f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, maxUint256Int, collected.AmountOf(denom))

	var addErr error
	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(one))
	require.NotPanics(t, func() {
		addErr = f.keeper.CollectFee(f.ctx, exchange.GetId(), one)
	})
	require.ErrorIs(t, addErr, types.ErrInvariantViolation)
	collected, err = f.keeper.GetCollectedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, maxUint256Int, collected.AmountOf(denom))
	require.Equal(t, sdk.NewCoins(one), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr))

	require.NoError(t, f.keeper.lockedFees.Set(
		f.ctx,
		exchange.GetId(),
		coinsToLedger(sdk.NewCoins(sdk.NewCoin(denom, maxMinusOne))),
	))
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), one))
	locked, err := f.keeper.GetLockedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, maxUint256Int, locked.AmountOf(denom))

	var lockErr error
	require.NotPanics(t, func() {
		lockErr = f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), one)
	})
	require.ErrorIs(t, lockErr, types.ErrInvariantViolation)
	locked, err = f.keeper.GetLockedFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, maxUint256Int, locked.AmountOf(denom))
}

func TestFeeInvariantDetectsOrphanAndCrossExchangeOverflow(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	first := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	require.NoError(t, f.keeper.collectedFees.Remove(f.ctx, first.GetId()))
	require.NoError(t, f.keeper.lockedFees.Set(
		f.ctx,
		first.GetId(),
		coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))),
	))
	require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)

	require.NoError(t, f.keeper.lockedFees.Set(f.ctx, first.GetId(), coinsToLedger(sdk.Coins{})))
	require.NoError(t, f.keeper.collectedFees.Set(
		f.ctx,
		first.GetId(),
		coinsToLedger(sdk.NewCoins(sdk.NewCoin("agxn", maxUint256Int))),
	))
	second := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.collectedFees.Set(
		f.ctx,
		second.GetId(),
		coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))),
	))
	require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
}

func TestExchangeIDAndRevisionNeverWrapUint64(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	require.NoError(t, f.keeper.setNextExchangeID(f.ctx, ^uint64(0)-1))
	id, err := f.keeper.nextID(f.ctx)
	require.NoError(t, err)
	require.Equal(t, ^uint64(0)-1, id)
	next, err := f.keeper.nextExchangeID.Peek(f.ctx)
	require.NoError(t, err)
	require.Equal(t, ^uint64(0), next)
	_, err = f.keeper.nextID(f.ctx)
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	next, err = f.keeper.nextExchangeID.Peek(f.ctx)
	require.NoError(t, err)
	require.Equal(t, ^uint64(0), next)

	terminalRevision, err := incrementRevision(^uint64(0) - 1)
	require.NoError(t, err)
	require.Equal(t, ^uint64(0), terminalRevision)
	_, err = incrementRevision(^uint64(0))
	require.ErrorIs(t, err, types.ErrRevisionConflict)
	_, err = incrementRevision(0)
	require.ErrorIs(t, err, types.ErrRevisionConflict)

	require.NoError(t, f.keeper.setNextExchangeID(f.ctx, DefaultNextExchangeID))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	terminal := cloneExchange(exchange)
	terminal.Revision = ^uint64(0)
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, terminal.GetId(), terminal))
	_, err = f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		terminal.GetId(),
		terminal.GetRevision(),
		&bexv1.ExchangeUpdatePatch{FeeBpsAToB: wrapperspb.UInt32(terminal.GetFeeBpsAToB() + 1)},
	)
	require.ErrorIs(t, err, types.ErrRevisionConflict)
	stored, err := f.keeper.GetExchange(f.ctx, terminal.GetId())
	require.NoError(t, err)
	require.Equal(t, ^uint64(0), stored.GetRevision())
}

func TestReserveIndexesAndDepositorsAreCoveredByInvariant(t *testing.T) {
	newFixture := func(t *testing.T) (keeperTestFixture, *bexv1.Exchange) {
		t.Helper()
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
		require.NoError(t, f.keeper.AssertInvariants(f.ctx))
		return f, exchange
	}

	t.Run("non-canonical admin", func(t *testing.T) {
		f, _ := newFixture(t)
		require.NoError(t, f.keeper.admins.Set(f.ctx, "not-an-address"))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("exchange payload id mismatch", func(t *testing.T) {
		f, exchange := newFixture(t)
		corrupted := cloneExchange(exchange)
		corrupted.Id++
		require.NoError(t, f.keeper.exchanges.Set(f.ctx, exchange.GetId(), corrupted))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("missing admin index", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.exchangesByAdmin.Remove(f.ctx, collections.Join(f.admin, exchange.GetId())))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("orphan admin index", func(t *testing.T) {
		f, _ := newFixture(t)
		require.NoError(t, f.keeper.exchangesByAdmin.Set(f.ctx, collections.Join(f.admin, uint64(999))))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("orphan reserve index", func(t *testing.T) {
		f, _ := newFixture(t)
		require.NoError(t, f.keeper.reserveByAddress.Set(f.ctx, "orphan-reserve", 999))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("orphan reserve depositor", func(t *testing.T) {
		f, _ := newFixture(t)
		require.NoError(t, f.keeper.reserveDepositors.Set(f.ctx, collections.Join(uint64(999), f.admin)))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("invalid volume window", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.volumeWindow.Set(
			f.ctx,
			collections.Join4(
				uint64(0),
				exchange.GetId(),
				uint32(bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B),
				collections.Join(minVolumeEpochSecs, exchange.GetVolumeWindowGeneration()),
			),
			"invalid",
		))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("misaligned volume window", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.volumeWindow.Set(
			f.ctx,
			volumeWindowKeyFromStart(
				exchange.GetId(),
				bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
				uint64(1),
				minVolumeEpochSecs,
				exchange.GetVolumeWindowGeneration(),
			),
			"1",
		))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("unbacked collected fee ledger", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.collectedFees.Set(
			f.ctx,
			exchange.GetId(),
			coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))),
		))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("unsupported collected fee denom", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.collectedFees.Set(
			f.ctx,
			exchange.GetId(),
			coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("unsupported", 1))),
		))
		f.bankKeeper.SetBalance(
			authtypes.NewModuleAddress(types.ModuleName),
			sdk.NewCoins(sdk.NewInt64Coin("unsupported", 1)),
		)
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("deleted exchange has a non-zero reserve", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()))
		require.NoError(t, f.keeper.AssertInvariants(f.ctx))
		f.bankKeeper.SetBalance(
			f.keeper.GetReserveAddress(f.ctx, exchange.GetId()),
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		)
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("deleted exchange has a non-zero fee ledger", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()))
		require.NoError(t, f.keeper.collectedFees.Set(
			f.ctx,
			exchange.GetId(),
			coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))),
		))
		f.bankKeeper.SetBalance(
			authtypes.NewModuleAddress(types.ModuleName),
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		)
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("unsafe inactive reserve account", func(t *testing.T) {
		f, exchange := newFixture(t)
		reserve := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
		f.accountKeeper.SetAccount(f.ctx, authtypes.NewEmptyModuleAccount(ReserveModuleName(exchange.GetId())))
		require.NotNil(t, f.accountKeeper.GetAccount(f.ctx, reserve))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})
}

func TestInvariantRejectsIncompleteTombstoneAndNonCanonicalAmounts(t *testing.T) {
	newFixture := func(t *testing.T) (keeperTestFixture, *bexv1.Exchange) {
		t.Helper()
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
		return f, exchange
	}

	t.Run("incomplete tombstone", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()))
		deleted, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
		require.NoError(t, err)
		deleted.VolumeEpochSeconds = 0
		require.NoError(t, f.keeper.exchanges.Set(f.ctx, deleted.GetId(), deleted))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("exchange limit", func(t *testing.T) {
		f, exchange := newFixture(t)
		corrupted := cloneExchange(exchange)
		corrupted.LimitAToB = "01"
		require.NoError(t, f.keeper.exchanges.Set(f.ctx, exchange.GetId(), corrupted))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("volume amount", func(t *testing.T) {
		f, exchange := newFixture(t)
		key := currentVolumeKey(
			f.ctx.BlockTime(),
			exchange.GetId(),
			bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
			exchange.GetVolumeEpochSeconds(),
			exchange.GetVolumeWindowGeneration(),
		)
		require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, key, "01"))
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("collected fee amount", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.collectedFees.Set(f.ctx, exchange.GetId(), &bexv1.FeeLedger{
			Coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "01"}},
		}))
		f.bankKeeper.SetBalance(
			authtypes.NewModuleAddress(types.ModuleName),
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		)
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})

	t.Run("locked fee amount", func(t *testing.T) {
		f, exchange := newFixture(t)
		require.NoError(t, f.keeper.collectedFees.Set(
			f.ctx,
			exchange.GetId(),
			coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))),
		))
		require.NoError(t, f.keeper.lockedFees.Set(f.ctx, exchange.GetId(), &bexv1.FeeLedger{
			Coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "01"}},
		}))
		f.bankKeeper.SetBalance(
			authtypes.NewModuleAddress(types.ModuleName),
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		)
		require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
	})
}
