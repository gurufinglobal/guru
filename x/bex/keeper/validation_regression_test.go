package keeper

import (
	"math"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestExchangeDenomIdentitiesAreFourWayDistinct(t *testing.T) {
	collisionCases := []struct {
		name   string
		mutate func(t *testing.T, msg *types.MsgRegisterExchange)
	}{
		{
			name: "denom_a_matches_derived_ibc_denom_b",
			mutate: func(t *testing.T, msg *types.MsgRegisterExchange) {
				t.Helper()
				ibcDenomB, err := buildIBCDenom(msg.GetDenomB(), msg.GetPortB(), msg.GetChannelB())
				require.NoError(t, err)
				msg.DenomA = ibcDenomB
			},
		},
		{
			name: "denom_b_matches_derived_ibc_denom_a",
			mutate: func(t *testing.T, msg *types.MsgRegisterExchange) {
				t.Helper()
				ibcDenomA, err := buildIBCDenom(msg.GetDenomA(), msg.GetPortA(), msg.GetChannelA())
				require.NoError(t, err)
				msg.DenomB = ibcDenomA
			},
		},
	}

	for _, tc := range collisionCases {
		t.Run("register/"+tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			msg := validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
			tc.mutate(t, msg)

			_, err := f.keeper.RegisterExchange(f.ctx, msg)
			require.ErrorIs(t, err, types.ErrInvalidRoute)
		})
	}

	updateCases := []struct {
		name  string
		patch func(*types.Exchange) *types.ExchangeUpdatePatch
	}{
		{
			name: "denom_a_matches_derived_ibc_denom_b",
			patch: func(exchange *types.Exchange) *types.ExchangeUpdatePatch {
				return &types.ExchangeUpdatePatch{DenomA: types.NewStringValue(exchange.GetIbcDenomB())}
			},
		},
		{
			name: "denom_b_matches_derived_ibc_denom_a",
			patch: func(exchange *types.Exchange) *types.ExchangeUpdatePatch {
				return &types.ExchangeUpdatePatch{DenomB: types.NewStringValue(exchange.GetIbcDenomA())}
			},
		},
	}

	for _, tc := range updateCases {
		t.Run("inactive_update/"+tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

			_, err := f.keeper.UpdateExchange(
				f.ctx,
				f.admin,
				exchange.GetId(),
				exchange.GetRevision(),
				tc.patch(exchange),
			)
			require.ErrorIs(t, err, types.ErrInvalidRoute)

			stored, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
			require.NoError(t, err)
			require.True(t, types.EqualMessages(exchange, stored), "failed update must not mutate the exchange")
		})
	}
}

func TestPendingVolumeEpochEffectiveAtRejectsBeyondInt64(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	_, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&types.ExchangeUpdatePatch{
			PendingVolumeEpochSeconds:         types.NewUInt32Value(minVolumeEpochSecs * 2),
			PendingVolumeEpochEffectiveAtUnix: types.NewUInt64Value(uint64(math.MaxInt64) + 1),
		},
	)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	stored, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(exchange, stored), "failed update must not mutate the exchange")
}

func TestPendingVolumeEpochRequiresFutureCounters(t *testing.T) {
	for _, tc := range []struct {
		name          string
		corrupt       func(*types.Exchange)
		expectedError error
	}{
		{
			name: "revision",
			corrupt: func(exchange *types.Exchange) {
				exchange.Revision = ^uint64(0) - 1
			},
			expectedError: types.ErrRevisionConflict,
		},
		{
			name: "generation",
			corrupt: func(exchange *types.Exchange) {
				exchange.VolumeWindowGeneration = ^uint64(0)
			},
			expectedError: types.ErrInvalidRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
			corrupted := cloneExchange(exchange)
			tc.corrupt(corrupted)
			require.NoError(t, f.keeper.exchanges.Set(f.ctx, corrupted.GetId(), corrupted))

			_, err := f.keeper.UpdateExchange(
				f.ctx,
				f.admin,
				corrupted.GetId(),
				corrupted.GetRevision(),
				&types.ExchangeUpdatePatch{
					PendingVolumeEpochSeconds:         types.NewUInt32Value(minVolumeEpochSecs * 2),
					PendingVolumeEpochEffectiveAtUnix: types.NewUInt64Value(uint64(f.ctx.BlockTime().Unix()) + 1),
				},
			)
			require.ErrorIs(t, err, tc.expectedError)
			persisted, err := f.keeper.GetExchange(f.ctx, corrupted.GetId())
			require.NoError(t, err)
			require.True(t, types.EqualMessages(corrupted, persisted))
		})
	}
}

func TestPendingVolumeEpochRejectsFinalGenerationExhaustion(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	exhausting := cloneExchange(exchange)
	exhausting.VolumeWindowGeneration = ^uint64(0) - 1
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, exhausting.GetId(), exhausting))

	_, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exhausting.GetId(),
		exhausting.GetRevision(),
		&types.ExchangeUpdatePatch{
			VolumeEpochSeconds:                types.NewUInt32Value(exhausting.GetVolumeEpochSeconds() * 2),
			PendingVolumeEpochSeconds:         types.NewUInt32Value(exhausting.GetVolumeEpochSeconds() * 3),
			PendingVolumeEpochEffectiveAtUnix: types.NewUInt64Value(uint64(f.ctx.BlockTime().Unix()) + 1),
		},
	)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	persisted, err := f.keeper.GetExchange(f.ctx, exhausting.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(exhausting, persisted), "failed update must not persist an unactivatable pending schedule")
}

func TestStoredFeeLedgerInvariantRejectsMalformedAndUnsorted(t *testing.T) {
	tests := []struct {
		name  string
		store func(f keeperTestFixture, exchangeID uint64, ledger *types.FeeLedger) error
		coins sdk.Coins
	}{
		{
			name: "malformed_collected",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *types.FeeLedger) error {
				return f.keeper.collectedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: sdk.Coins{{Denom: "bad denom", Amount: sdkmath.OneInt()}},
		},
		{
			name: "unsorted_collected",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *types.FeeLedger) error {
				return f.keeper.collectedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: sdk.Coins{
				sdk.NewInt64Coin("gxusd", 1),
				sdk.NewInt64Coin("agxn", 1),
			},
		},
		{
			name: "malformed_locked",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *types.FeeLedger) error {
				return f.keeper.lockedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: sdk.Coins{{Denom: "bad denom", Amount: sdkmath.OneInt()}},
		},
		{
			name: "unsorted_locked",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *types.FeeLedger) error {
				return f.keeper.lockedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: sdk.Coins{
				sdk.NewInt64Coin("gxusd", 1),
				sdk.NewInt64Coin("agxn", 1),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
			require.NoError(t, tc.store(f, exchange.GetId(), &types.FeeLedger{Coins: tc.coins}))

			require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
		})
	}
}
