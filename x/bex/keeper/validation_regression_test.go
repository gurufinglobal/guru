package keeper

import (
	"math"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestExchangeDenomIdentitiesAreFourWayDistinct(t *testing.T) {
	collisionCases := []struct {
		name   string
		mutate func(t *testing.T, msg *bexv1.MsgRegisterExchange)
	}{
		{
			name: "denom_a_matches_derived_ibc_denom_b",
			mutate: func(t *testing.T, msg *bexv1.MsgRegisterExchange) {
				t.Helper()
				ibcDenomB, err := buildIBCDenom(msg.GetDenomB(), msg.GetPortB(), msg.GetChannelB())
				require.NoError(t, err)
				msg.DenomA = ibcDenomB
			},
		},
		{
			name: "denom_b_matches_derived_ibc_denom_a",
			mutate: func(t *testing.T, msg *bexv1.MsgRegisterExchange) {
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
			msg := validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
			tc.mutate(t, msg)

			_, err := f.keeper.RegisterExchange(f.ctx, msg)
			require.ErrorIs(t, err, types.ErrInvalidRoute)
		})
	}

	updateCases := []struct {
		name  string
		patch func(*bexv1.Exchange) *bexv1.ExchangeUpdatePatch
	}{
		{
			name: "denom_a_matches_derived_ibc_denom_b",
			patch: func(exchange *bexv1.Exchange) *bexv1.ExchangeUpdatePatch {
				return &bexv1.ExchangeUpdatePatch{DenomA: wrapperspb.String(exchange.GetIbcDenomB())}
			},
		},
		{
			name: "denom_b_matches_derived_ibc_denom_a",
			patch: func(exchange *bexv1.Exchange) *bexv1.ExchangeUpdatePatch {
				return &bexv1.ExchangeUpdatePatch{DenomB: wrapperspb.String(exchange.GetIbcDenomA())}
			},
		},
	}

	for _, tc := range updateCases {
		t.Run("inactive_update/"+tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

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
			require.True(t, proto.Equal(exchange, stored), "failed update must not mutate the exchange")
		})
	}
}

func TestPendingVolumeEpochEffectiveAtRejectsBeyondInt64(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	_, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exchange.GetId(),
		exchange.GetRevision(),
		&bexv1.ExchangeUpdatePatch{
			PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 2),
			PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(uint64(math.MaxInt64) + 1),
		},
	)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	stored, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, proto.Equal(exchange, stored), "failed update must not mutate the exchange")
}

func TestPendingVolumeEpochRequiresFutureCounters(t *testing.T) {
	for _, tc := range []struct {
		name          string
		corrupt       func(*bexv1.Exchange)
		expectedError error
	}{
		{
			name: "revision",
			corrupt: func(exchange *bexv1.Exchange) {
				exchange.Revision = ^uint64(0) - 1
			},
			expectedError: types.ErrRevisionConflict,
		},
		{
			name: "generation",
			corrupt: func(exchange *bexv1.Exchange) {
				exchange.VolumeWindowGeneration = ^uint64(0)
			},
			expectedError: types.ErrInvalidRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
			corrupted := cloneExchange(exchange)
			tc.corrupt(corrupted)
			require.NoError(t, f.keeper.exchanges.Set(f.ctx, corrupted.GetId(), corrupted))

			_, err := f.keeper.UpdateExchange(
				f.ctx,
				f.admin,
				corrupted.GetId(),
				corrupted.GetRevision(),
				&bexv1.ExchangeUpdatePatch{
					PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 2),
					PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(uint64(f.ctx.BlockTime().Unix()) + 1),
				},
			)
			require.ErrorIs(t, err, tc.expectedError)
			persisted, err := f.keeper.GetExchange(f.ctx, corrupted.GetId())
			require.NoError(t, err)
			require.True(t, proto.Equal(corrupted, persisted))
		})
	}
}

func TestPendingVolumeEpochRejectsFinalGenerationExhaustion(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	exhausting := cloneExchange(exchange)
	exhausting.VolumeWindowGeneration = ^uint64(0) - 1
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, exhausting.GetId(), exhausting))

	_, err := f.keeper.UpdateExchange(
		f.ctx,
		f.admin,
		exhausting.GetId(),
		exhausting.GetRevision(),
		&bexv1.ExchangeUpdatePatch{
			VolumeEpochSeconds:                wrapperspb.UInt32(exhausting.GetVolumeEpochSeconds() * 2),
			PendingVolumeEpochSeconds:         wrapperspb.UInt32(exhausting.GetVolumeEpochSeconds() * 3),
			PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(uint64(f.ctx.BlockTime().Unix()) + 1),
		},
	)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	persisted, err := f.keeper.GetExchange(f.ctx, exhausting.GetId())
	require.NoError(t, err)
	require.True(t, proto.Equal(exhausting, persisted), "failed update must not persist an unactivatable pending schedule")
}

func TestStoredFeeLedgerInvariantRejectsMalformedAndUnsorted(t *testing.T) {
	tests := []struct {
		name  string
		store func(f keeperTestFixture, exchangeID uint64, ledger *bexv1.FeeLedger) error
		coins []*basev1beta1.Coin
	}{
		{
			name: "malformed_collected",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *bexv1.FeeLedger) error {
				return f.keeper.collectedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}},
		},
		{
			name: "unsorted_collected",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *bexv1.FeeLedger) error {
				return f.keeper.collectedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: []*basev1beta1.Coin{
				{Denom: "gxusd", Amount: "1"},
				{Denom: "agxn", Amount: "1"},
			},
		},
		{
			name: "malformed_locked",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *bexv1.FeeLedger) error {
				return f.keeper.lockedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}},
		},
		{
			name: "unsorted_locked",
			store: func(f keeperTestFixture, exchangeID uint64, ledger *bexv1.FeeLedger) error {
				return f.keeper.lockedFees.Set(f.ctx, exchangeID, ledger)
			},
			coins: []*basev1beta1.Coin{
				{Denom: "gxusd", Amount: "1"},
				{Denom: "agxn", Amount: "1"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
			require.NoError(t, tc.store(f, exchange.GetId(), &bexv1.FeeLedger{Coins: tc.coins}))

			require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
		})
	}
}
