package keeper

import (
	"context"
	"fmt"
	"testing"
	"time"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestAdminAuthorizationLifecycle(t *testing.T) {
	f := setupKeeperFixture(t)
	other, _ := testAddress(t, f.accountCodec, 0x04)

	ok, err := f.keeper.IsAdmin(f.ctx, f.admin)
	require.NoError(t, err)
	require.False(t, ok)
	_, err = f.keeper.IsAdmin(f.ctx, "not-an-address")
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	require.ErrorIs(t, f.keeper.RegisterAdmin(f.ctx, other, f.admin), types.ErrInvalidModerator)
	require.ErrorIs(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, "not-an-address"), types.ErrInvalidRequest)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	require.ErrorIs(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin), types.ErrInvalidRequest)

	ok, err = f.keeper.IsAdmin(f.ctx, f.admin)
	require.NoError(t, err)
	require.True(t, ok)

	require.ErrorIs(t, f.keeper.RemoveAdmin(f.ctx, other, f.admin), types.ErrInvalidModerator)
	require.ErrorIs(t, f.keeper.RemoveAdmin(f.ctx, f.moderator, other), types.ErrAdminNotFound)
	require.ErrorIs(t, f.keeper.RemoveAdmin(f.ctx, f.moderator, "not-an-address"), types.ErrInvalidRequest)
	require.NoError(t, f.keeper.RemoveAdmin(f.ctx, f.moderator, f.admin))
}

func TestAdminQueriesUseDistinctRoles(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchangeAdmin, _ := testAddress(t, f.accountCodec, 0x04)
	msg := validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	msg.ExchangeAdminAddress = exchangeAdmin
	exchange, err := f.keeper.RegisterExchange(f.ctx, msg)
	require.NoError(t, err)

	q := NewQueryServer(&f.keeper)
	owned, err := q.ExchangesByExchangeAdmin(f.ctx, &bexv1.QueryExchangesByExchangeAdminRequest{
		ExchangeAdminAddress: exchangeAdmin,
	})
	require.NoError(t, err)
	require.Len(t, owned.GetExchanges(), 1)
	require.Equal(t, exchange.GetId(), owned.GetExchanges()[0].GetId())

	registrarOwned, err := q.ExchangesByExchangeAdmin(f.ctx, &bexv1.QueryExchangesByExchangeAdminRequest{
		ExchangeAdminAddress: f.admin,
	})
	require.NoError(t, err)
	require.Empty(t, registrarOwned.GetExchanges())

	registrarStatus, err := q.IsBexAdmin(f.ctx, &bexv1.QueryIsBexAdminRequest{BexAdminAddress: f.admin})
	require.NoError(t, err)
	require.True(t, registrarStatus.GetIsBexAdmin())
	ownerStatus, err := q.IsBexAdmin(f.ctx, &bexv1.QueryIsBexAdminRequest{BexAdminAddress: exchangeAdmin})
	require.NoError(t, err)
	require.False(t, ownerStatus.GetIsBexAdmin())
}

func TestRegisterExchangeDefaultsAndValidation(t *testing.T) {
	f := setupKeeperFixture(t)
	_, err := f.keeper.RegisterExchange(f.ctx, validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE))
	require.ErrorIs(t, err, types.ErrAdminNotFound)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	defaulted := validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_UNSPECIFIED)
	defaulted.LimitAToB = " "
	defaulted.VolumeCapBToA = ""
	defaulted.Metadata = nil
	exchange, err := f.keeper.RegisterExchange(f.ctx, defaulted)
	require.NoError(t, err)
	require.Equal(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE, exchange.GetStatus())
	require.Equal(t, "0", exchange.GetLimitAToB())
	require.Equal(t, "0", exchange.GetVolumeCapBToA())
	require.Nil(t, exchange.GetMetadata())

	cases := []struct {
		name    string
		mutate  func(*bexv1.MsgRegisterExchange)
		wantErr error
	}{
		{
			name:    "bad denom",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.DenomA = "bad denom" },
			wantErr: types.ErrInvalidRoute,
		},
		{
			name: "closed active channel",
			mutate: func(msg *bexv1.MsgRegisterExchange) {
				f.channelKeeper.SetChannel("transwap", "channel-4", channeltypes.CLOSED)
				msg.ChannelA = "channel-4"
			},
			wantErr: types.ErrInvalidRoute,
		},
		{
			name:    "same denoms",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.DenomB = msg.DenomA },
			wantErr: types.ErrInvalidRoute,
		},
		{
			name:    "empty oracle",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.OracleSymbolAToB = " " },
			wantErr: types.ErrInvalidOracleRate,
		},
		{
			name:    "fee too high",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.FeeBpsAToB = 10000 },
			wantErr: types.ErrInvalidFeeBps,
		},
		{
			name:    "invalid integer",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.LimitAToB = "nan" },
			wantErr: types.ErrInvalidRequest,
		},
		{
			name: "metadata too many entries",
			mutate: func(msg *bexv1.MsgRegisterExchange) {
				msg.Metadata = map[string]string{}
				for i := 0; i <= maxMetadataEntries; i++ {
					msg.Metadata[fmt.Sprintf("k%d", i)] = "v"
				}
			},
			wantErr: types.ErrInvalidRequest,
		},
		{
			name:    "too short epoch",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.VolumeEpochSeconds = minVolumeEpochSecs - 1 },
			wantErr: types.ErrInvalidRequest,
		},
		{
			name:    "zero staleness",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.MaxOracleStalenessSeconds = 0 },
			wantErr: types.ErrInvalidRequest,
		},
		{
			name:    "deleted status",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.Status = bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED },
			wantErr: types.ErrInvalidRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
			tc.mutate(msg)
			_, err := f.keeper.RegisterExchange(f.ctx, msg)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestUpdateExchangeFullPatchAndErrors(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	other, _ := testAddress(t, f.accountCodec, 0x05)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, other))

	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		DenomA:                            wrapperspb.String("agxn"),
		PortA:                             wrapperspb.String("transwap"),
		ChannelA:                          wrapperspb.String("channel-2"),
		DenomB:                            wrapperspb.String("gxusd"),
		PortB:                             wrapperspb.String("transwap"),
		ChannelB:                          wrapperspb.String("channel-3"),
		OracleSymbolAToB:                  wrapperspb.String("OSMO/ETH"),
		OracleSymbolBToA:                  wrapperspb.String("ETH/OSMO"),
		FeeBpsAToB:                        wrapperspb.UInt32(11),
		FeeBpsBToA:                        wrapperspb.UInt32(12),
		LimitAToB:                         wrapperspb.String("101"),
		LimitBToA:                         wrapperspb.String("202"),
		VolumeCapAToB:                     wrapperspb.String("303"),
		VolumeCapBToA:                     wrapperspb.String("404"),
		Status:                            &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE},
		ClearMetadata:                     wrapperspb.Bool(true),
		Metadata:                          map[string]string{"desk": "one"},
		VolumeEpochSeconds:                wrapperspb.UInt32(minVolumeEpochSecs * 2),
		PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 3),
		PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(1_700_000_030),
		MaxOracleStalenessSeconds:         wrapperspb.UInt32(15),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), updated.GetRevision())
	require.Equal(t, "agxn", updated.GetDenomA())
	require.Equal(t, "gxusd", updated.GetDenomB())
	require.Equal(t, "OSMO/ETH", updated.GetOracleSymbolAToB())
	require.Equal(t, uint32(11), updated.GetFeeBpsAToB())
	require.Equal(t, "101", updated.GetLimitAToB())
	require.Equal(t, map[string]string{"desk": "one"}, updated.GetMetadata())
	require.Equal(t, minVolumeEpochSecs*2, updated.GetVolumeEpochSeconds())
	require.Equal(t, minVolumeEpochSecs*3, updated.GetPendingVolumeEpochSeconds())
	require.Equal(t, uint64(1_700_000_030), updated.GetPendingVolumeEpochEffectiveAtUnix())
	require.Equal(t, uint32(15), updated.GetMaxOracleStalenessSeconds())

	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE},
	})
	require.ErrorIs(t, err, types.ErrInvalidRoute)
	f.channelKeeper.SetChannel("transwap", "channel-2", channeltypes.OPEN)
	f.channelKeeper.SetChannel("transwap", "channel-3", channeltypes.OPEN)
	updated, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE},
	})
	require.NoError(t, err)
	require.Equal(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE, updated.GetStatus())

	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED},
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(updated.GetFeeBpsAToB()),
	})
	require.ErrorIs(t, err, types.ErrNoOpUpdate)

	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), nil)
	require.ErrorIs(t, err, types.ErrNoOpUpdate)

	_, err = f.keeper.UpdateExchange(f.ctx, other, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(13),
	})
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)

	badRouteExchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, badRouteExchange.GetId(), badRouteExchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		DenomA: wrapperspb.String("bad denom"),
	})
	require.ErrorIs(t, err, types.ErrInvalidRoute)

	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		DenomA: wrapperspb.String("bad denom"),
	})
	require.ErrorIs(t, err, types.ErrInvalidRoute)

	cleared, err := f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		ClearMetadata: wrapperspb.Bool(true),
	})
	require.NoError(t, err)
	require.Nil(t, cleared.GetMetadata())

	deleted := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, deleted.GetId()))
	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, deleted.GetId(), deleted.GetRevision()+1, &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(13),
	})
	require.ErrorIs(t, err, types.ErrExchangeDeleted)
}

func TestUpdateExchangeRejectsActivationWhenRouteChannelCloses(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	f.channelKeeper.SetChannel(exchange.GetPortA(), exchange.GetChannelA(), channeltypes.CLOSED)
	_, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE},
	})
	require.ErrorIs(t, err, types.ErrInvalidRoute)

	unchanged, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, exchange.GetRevision(), unchanged.GetRevision())
	require.Equal(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE, unchanged.GetStatus())

	f.channelKeeper.SetChannel(exchange.GetPortA(), exchange.GetChannelA(), channeltypes.OPEN)
	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE},
	})
	require.NoError(t, err)
	require.Equal(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE, updated.GetStatus())
}

func TestKeeperEmitsRequiredEvents(t *testing.T) {
	f := setupKeeperFixture(t)

	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(30),
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.DepositReserve(f.ctx, f.admin, updated.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 25))))
	require.NoError(t, f.keeper.CollectFee(f.ctx, updated.GetId(), sdk.NewInt64Coin("agxn", 10)))
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, updated.GetId(), sdk.NewInt64Coin("agxn", 3)))
	updated, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE},
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.WithdrawReserve(f.ctx, f.admin, updated.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 5))))
	require.NoError(t, f.keeper.ReleaseExchangeFee(f.ctx, updated.GetId(), sdk.NewInt64Coin("agxn", 1)))
	require.NoError(t, f.keeper.RefundLockedFee(f.ctx, updated.GetId(), sdk.NewInt64Coin("agxn", 2)))
	require.NoError(t, f.keeper.WithdrawFees(f.ctx, f.admin, updated.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))))

	updated, err = f.keeper.UpdateExchange(f.ctx, f.admin, updated.GetId(), updated.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE},
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, updated.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)))
	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, updated.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1000)), types.ErrVolumeCapExceeded)

	toDelete := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, toDelete.GetId()))
	newAdmin, _ := testAddress(t, f.accountCodec, 0x09)
	require.NoError(t, f.keeper.UpdateAdmin(f.ctx, f.moderator, f.admin, newAdmin))

	requireEventTypes(
		t,
		f.ctx,
		types.EventTypeAdminRegistered,
		types.EventTypeAdminUpdated,
		types.EventTypeExchangeRegistered,
		types.EventTypeExchangeUpdated,
		types.EventTypeExchangeStatus,
		types.EventTypeExchangeDeleted,
		types.EventTypeReserveDeposited,
		types.EventTypeReserveWithdrawn,
		types.EventTypeFeesCollected,
		types.EventTypeFeesLocked,
		types.EventTypeFeesReleased,
		types.EventTypeFeesRefunded,
		types.EventTypeFeesWithdrawn,
		types.EventTypeVolumeRecorded,
	)
}

func TestQuoteSwapErrorPathsAndBToA(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	active := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	inactive := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	_, err := f.keeper.QuoteSwap(f.ctx, nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: 999, InputDenom: "agxn", AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrExchangeNotFound)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: inactive.GetId(), InputDenom: "agxn", AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidRoute)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: "agxn", AmountIn: "0"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: "ubad", AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidRoute)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "0", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "1", f.ctx.BlockTime().Add(-time.Hour).Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrStaleOracleRate)

	active, err = f.keeper.UpdateExchange(f.ctx, f.admin, active.GetId(), active.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(9999),
	})
	require.NoError(t, err)
	f.oracleKeeper.SetValue("AGXN/GXUSD", "1", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	active, err = f.keeper.UpdateExchange(f.ctx, f.admin, active.GetId(), active.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(0),
		LimitAToB:  wrapperspb.String("1"),
	})
	require.NoError(t, err)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "2"})
	require.ErrorIs(t, err, types.ErrOutputLimitExceeded)

	active, err = f.keeper.UpdateExchange(f.ctx, f.admin, active.GetId(), active.GetRevision(), &bexv1.ExchangeUpdatePatch{
		LimitAToB: wrapperspb.String("0"),
	})
	require.NoError(t, err)
	f.oracleKeeper.SetValue("AGXN/GXUSD", "0.1", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: maxUint256String})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("GXUSD/AGXN", "3", f.ctx.BlockTime().Unix())
	quote, err := f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomB(), AmountIn: "10"})
	require.NoError(t, err)
	require.Equal(t, bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A, quote.GetDirection())
	require.Equal(t, active.GetIbcDenomA(), quote.GetOutputDenom())
}

func TestVolumeWindowAndQueries(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	ex1 := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	collectFee(t, f, ex1.GetId(), sdk.NewInt64Coin("agxn", 10))
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, ex1.GetId(), sdk.NewInt64Coin("agxn", 3)))
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, ex1.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(7)))
	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Unix())

	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, ex1.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.ZeroInt()), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, ex1.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, sdkmath.NewInt(1)), types.ErrInvalidRoute)

	q := NewQueryServer(&f.keeper)
	for name, call := range map[string]func() error{
		"exchange":                    func() error { _, err := q.Exchange(f.ctx, nil); return err },
		"exchanges":                   func() error { _, err := q.Exchanges(f.ctx, nil); return err },
		"exchanges_by_exchange_admin": func() error { _, err := q.ExchangesByExchangeAdmin(f.ctx, nil); return err },
		"is_bex_admin":                func() error { _, err := q.IsBexAdmin(f.ctx, nil); return err },
		"fees":                        func() error { _, err := q.CollectedFees(f.ctx, nil); return err },
		"volume":                      func() error { _, err := q.VolumeWindow(f.ctx, nil); return err },
		"quote":                       func() error { _, err := q.QuoteSwap(f.ctx, nil); return err },
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, call(), types.ErrInvalidRequest)
		})
	}

	exResp, err := q.Exchange(f.ctx, &bexv1.QueryExchangeRequest{ExchangeId: ex1.GetId()})
	require.NoError(t, err)
	require.Equal(t, ex1.GetId(), exResp.GetExchange().GetId())
	_, err = q.Exchange(f.ctx, &bexv1.QueryExchangeRequest{ExchangeId: 999})
	require.ErrorIs(t, err, types.ErrExchangeNotFound)

	byExchangeAdmin, err := q.ExchangesByExchangeAdmin(f.ctx, &bexv1.QueryExchangesByExchangeAdminRequest{
		ExchangeAdminAddress: f.admin,
		Pagination:           &queryv1beta1.PageRequest{Offset: 1, Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, byExchangeAdmin.GetExchanges(), 1)
	_, err = q.ExchangesByExchangeAdmin(f.ctx, &bexv1.QueryExchangesByExchangeAdminRequest{ExchangeAdminAddress: "bad"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	isBexAdmin, err := q.IsBexAdmin(f.ctx, &bexv1.QueryIsBexAdminRequest{BexAdminAddress: f.admin})
	require.NoError(t, err)
	require.True(t, isBexAdmin.GetIsBexAdmin())
	_, err = q.IsBexAdmin(f.ctx, &bexv1.QueryIsBexAdminRequest{BexAdminAddress: "bad"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	for name, call := range map[string]func() (*bexv1.QueryFeesResponse, error){
		"collected": func() (*bexv1.QueryFeesResponse, error) {
			return q.CollectedFees(f.ctx, &bexv1.QueryFeesRequest{ExchangeId: ex1.GetId()})
		},
		"locked": func() (*bexv1.QueryFeesResponse, error) {
			return q.LockedFees(f.ctx, &bexv1.QueryFeesRequest{ExchangeId: ex1.GetId()})
		},
		"available": func() (*bexv1.QueryFeesResponse, error) {
			return q.AvailableFees(f.ctx, &bexv1.QueryFeesRequest{ExchangeId: ex1.GetId()})
		},
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := call()
			require.NoError(t, err)
			require.NotNil(t, resp.GetLedger())
		})
	}
	_, err = q.CollectedFees(f.ctx, &bexv1.QueryFeesRequest{ExchangeId: 999})
	require.ErrorIs(t, err, types.ErrExchangeNotFound)

	window, err := q.VolumeWindow(f.ctx, &bexv1.QueryVolumeWindowRequest{ExchangeId: ex1.GetId(), Direction: bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B})
	require.NoError(t, err)
	require.Equal(t, "7", window.GetWindow().GetAmount())
	require.Equal(t, ex1.GetVolumeCapAToB(), window.GetCap())
	_, err = q.VolumeWindow(f.ctx, &bexv1.QueryVolumeWindowRequest{ExchangeId: ex1.GetId(), Direction: bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED})
	require.ErrorIs(t, err, types.ErrInvalidRoute)
	badVolumeKey := currentVolumeKey(
		f.ctx.BlockTime(),
		ex1.GetId(),
		bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED,
		ex1.GetVolumeEpochSeconds(),
		ex1.GetVolumeWindowGeneration(),
	)
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, badVolumeKey, "bad"))
	_, err = f.keeper.GetCurrentVolumeAmount(f.ctx, ex1, bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	quote, err := q.QuoteSwap(f.ctx, &bexv1.QueryQuoteSwapRequest{ExchangeId: ex1.GetId(), InputDenom: ex1.GetDenomA(), AmountIn: "2"})
	require.NoError(t, err)
	require.Equal(t, "2", quote.GetQuote().GetAmountOut())
	require.Equal(t, ex1.GetRevision(), quote.GetQuote().GetExchangeRevision())
}

func TestExchangeQueryFiltersTombstonesFromPrimaryState(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	first := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	deleted := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	last := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, deleted.GetId()))

	q := NewQueryServer(&f.keeper)
	invalidKeyPage, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{Pagination: &queryv1beta1.PageRequest{Key: []byte("bad")}})
	require.NoError(t, err)
	require.Empty(t, invalidKeyPage.GetExchanges())
	firstPage, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{
		Pagination: &queryv1beta1.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, firstPage.GetExchanges(), 1)
	require.Equal(t, first.GetId(), firstPage.GetExchanges()[0].GetId())
	require.NotEmpty(t, firstPage.GetPagination().GetNextKey())

	secondPage, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{
		Pagination: &queryv1beta1.PageRequest{Key: firstPage.GetPagination().GetNextKey(), Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, secondPage.GetExchanges(), 1)
	require.Equal(t, last.GetId(), secondPage.GetExchanges()[0].GetId())

	reverse, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{
		Pagination: &queryv1beta1.PageRequest{Limit: 2, Reverse: true},
	})
	require.NoError(t, err)
	require.Len(t, reverse.GetExchanges(), 2)
	require.Equal(t, []uint64{last.GetId(), first.GetId()}, []uint64{
		reverse.GetExchanges()[0].GetId(),
		reverse.GetExchanges()[1].GetId(),
	})

	all, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{
		IncludeDeleted: true,
		Pagination:     &queryv1beta1.PageRequest{Limit: 3},
	})
	require.NoError(t, err)
	require.Len(t, all.GetExchanges(), 3)
	require.Equal(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED, all.GetExchanges()[1].GetStatus())

	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, first.GetId()))
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, last.GetId()))
	empty, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{})
	require.NoError(t, err)
	require.Empty(t, empty.GetExchanges())
}

func TestImmediateVolumeEpochUpdateStartsNewWindowAndPreservesPendingSchedule(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B
	oldKey := currentVolumeKey(
		f.ctx.BlockTime(),
		exchange.GetId(),
		direction,
		exchange.GetVolumeEpochSeconds(),
		exchange.GetVolumeWindowGeneration(),
	)
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), direction, sdkmath.NewInt(10)))

	effectiveAt := uint64(f.ctx.BlockTime().Add(time.Hour).Unix())
	scheduled, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 2),
		PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(effectiveAt),
	})
	require.NoError(t, err)
	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), scheduled.GetRevision(), &bexv1.ExchangeUpdatePatch{
		VolumeEpochSeconds: wrapperspb.UInt32(minVolumeEpochSecs * 2),
	})
	require.NoError(t, err)
	require.Equal(t, scheduled.GetPendingVolumeEpochSeconds(), updated.GetPendingVolumeEpochSeconds())
	require.Equal(t, scheduled.GetPendingVolumeEpochEffectiveAtUnix(), updated.GetPendingVolumeEpochEffectiveAtUnix())

	used, err := f.keeper.GetCurrentVolumeAmount(f.ctx, updated, direction)
	require.NoError(t, err)
	require.True(t, used.IsZero())
	oldUsage, err := f.keeper.volumeWindow.Get(f.ctx, oldKey)
	require.NoError(t, err)
	require.Equal(t, "10", oldUsage)

	dueCtx := f.ctx.WithBlockTime(time.Unix(int64(effectiveAt), 0))
	require.NoError(t, f.keeper.RecordVolumeWindow(dueCtx, exchange.GetId(), direction, sdkmath.OneInt()))
	persisted, err := f.keeper.GetExchange(dueCtx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, minVolumeEpochSecs*2, persisted.GetVolumeEpochSeconds())
	require.Zero(t, persisted.GetPendingVolumeEpochSeconds())
	require.Zero(t, persisted.GetPendingVolumeEpochEffectiveAtUnix())
	require.Equal(t, updated.GetRevision()+1, persisted.GetRevision())
}

func TestExchangesByExchangeAdminRejectsCorruptOwnerIndex(t *testing.T) {
	t.Run("orphan index", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		require.NoError(t, f.keeper.exchangesByAdmin.Set(f.ctx, collections.Join(f.admin, uint64(999))))

		_, err := NewQueryServer(&f.keeper).ExchangesByExchangeAdmin(f.ctx, &bexv1.QueryExchangesByExchangeAdminRequest{
			ExchangeAdminAddress: f.admin,
		})
		require.ErrorIs(t, err, types.ErrInvariantViolation)
	})

	t.Run("owner mismatch", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
		other, _ := testAddress(t, f.accountCodec, 0x29)
		corrupted := cloneExchange(exchange)
		corrupted.AdminAddress = other
		require.NoError(t, f.keeper.exchanges.Set(f.ctx, exchange.GetId(), corrupted))

		_, err := NewQueryServer(&f.keeper).ExchangesByExchangeAdmin(f.ctx, &bexv1.QueryExchangesByExchangeAdminRequest{
			ExchangeAdminAddress: f.admin,
		})
		require.ErrorIs(t, err, types.ErrInvariantViolation)
	})
}

func TestAuthzDispatchRequiresBexAdminSignerField(t *testing.T) {
	authzKey := storetypes.NewKVStoreKey(authzkeeper.StoreKey)
	f := setupKeeperFixtureWithExtraKVStoreKeys(t, map[string]*storetypes.KVStoreKey{authzkeeper.StoreKey: authzKey})
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	grantee, granteeAddr := testAddress(t, f.accountCodec, 0x17)
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, granteeAddr))

	authzKeeper := newBexAuthzKeeper(t, f, authzKey)
	msgType := sdk.MsgTypeURL(&bexv1.MsgRegisterExchange{})
	adminMsg := validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	_, err := authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{adminMsg})
	require.ErrorIs(t, err, authz.ErrNoAuthorizationFound)

	expiration := f.ctx.BlockTime().Add(time.Hour)
	require.NoError(t, authzKeeper.SaveGrant(
		f.ctx,
		granteeAddr,
		f.adminAddr,
		authz.NewGenericAuthorization(msgType),
		&expiration,
	))

	granteeMsg := validRegisterExchangeMsg(grantee, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	_, err = authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{granteeMsg})
	require.ErrorIs(t, err, types.ErrAdminNotFound)

	_, err = f.keeper.GetExchange(f.ctx, 1)
	require.ErrorIs(t, err, types.ErrExchangeNotFound)

	_, err = authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{adminMsg})
	require.NoError(t, err)

	exchange, err := f.keeper.GetExchange(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, f.admin, exchange.GetAdminAddress())
}

func TestAuthzDispatchUsesBexSignerFieldsForReserveAndFeeMessages(t *testing.T) {
	authzKey := storetypes.NewKVStoreKey(authzkeeper.StoreKey)
	f := setupKeeperFixtureWithExtraKVStoreKeys(t, map[string]*storetypes.KVStoreKey{authzkeeper.StoreKey: authzKey})
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	grantee, granteeAddr := testAddress(t, f.accountCodec, 0x18)
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, granteeAddr))

	authzKeeper := newBexAuthzKeeper(t, f, authzKey)
	expiration := f.ctx.BlockTime().Add(time.Hour)
	grant := func(msg sdk.Msg) {
		t.Helper()
		require.NoError(t, authzKeeper.SaveGrant(
			f.ctx,
			granteeAddr,
			f.adminAddr,
			authz.NewGenericAuthorization(sdk.MsgTypeURL(msg)),
			&expiration,
		))
	}

	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reserveAddr := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
	amount := []*basev1beta1.Coin{{Denom: "agxn", Amount: "9"}}

	deposit := &bexv1.MsgDepositReserve{Sender: f.admin, ExchangeId: exchange.GetId(), Amount: amount}
	grant(deposit)
	_, err := authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{
		&bexv1.MsgDepositReserve{Sender: grantee, ExchangeId: exchange.GetId(), Amount: amount},
	})
	require.ErrorIs(t, err, types.ErrUnauthorizedReserveDepositor)
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).IsZero())

	_, err = authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{deposit})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(9), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))
	require.NoError(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 4)))
	require.Equal(t, sdkmath.NewInt(5), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))

	inactive, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE},
	})
	require.NoError(t, err)
	recipient, err := f.accountCodec.BytesToString(f.recipient)
	require.NoError(t, err)

	withdrawReserve := &bexv1.MsgWithdrawReserve{AdminAddress: f.admin, ExchangeId: inactive.GetId(), Amount: []*basev1beta1.Coin{{Denom: "agxn", Amount: "2"}}, Recipient: recipient}
	grant(withdrawReserve)
	_, err = authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{
		&bexv1.MsgWithdrawReserve{AdminAddress: grantee, ExchangeId: inactive.GetId(), Amount: withdrawReserve.GetAmount(), Recipient: recipient},
	})
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, f.recipient).IsZero())
	require.Equal(t, sdkmath.NewInt(5), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))

	_, err = authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{withdrawReserve})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(2), f.bankKeeper.GetAllBalances(f.ctx, f.recipient).AmountOf("agxn"))
	require.Equal(t, sdkmath.NewInt(3), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))

	withdrawFees := &bexv1.MsgWithdrawFees{AdminAddress: f.admin, ExchangeId: inactive.GetId(), Amount: []*basev1beta1.Coin{{Denom: "agxn", Amount: "3"}}, Recipient: recipient}
	grant(withdrawFees)
	_, err = authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{
		&bexv1.MsgWithdrawFees{AdminAddress: grantee, ExchangeId: inactive.GetId(), Amount: withdrawFees.GetAmount(), Recipient: recipient},
	})
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	require.Equal(t, sdkmath.NewInt(2), f.bankKeeper.GetAllBalances(f.ctx, f.recipient).AmountOf("agxn"))
	collected, err := f.keeper.GetCollectedFees(f.ctx, inactive.GetId())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 4)), collected)

	_, err = authzKeeper.DispatchActions(f.ctx, granteeAddr, []sdk.Msg{withdrawFees})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(5), f.bankKeeper.GetAllBalances(f.ctx, f.recipient).AmountOf("agxn"))
	require.Equal(t, sdkmath.NewInt(1), f.bankKeeper.GetAllBalances(f.ctx, authtypes.NewModuleAddress(types.ModuleName)).AmountOf("agxn"))
	collected, err = f.keeper.GetCollectedFees(f.ctx, inactive.GetId())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)), collected)
}

func TestAuthzMsgExecRequiresBexAdminSignerFieldForRegisterExchange(t *testing.T) {
	authzKey := storetypes.NewKVStoreKey(authzkeeper.StoreKey)
	f := setupKeeperFixtureWithExtraKVStoreKeys(t, map[string]*storetypes.KVStoreKey{authzkeeper.StoreKey: authzKey})
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	grantee, granteeAddr := testAddress(t, f.accountCodec, 0x1a)
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, granteeAddr))

	authzKeeper := newBexAuthzKeeper(t, f, authzKey)
	expiration := f.ctx.BlockTime().Add(time.Hour)
	adminMsg := validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, authzKeeper.SaveGrant(
		f.ctx,
		granteeAddr,
		f.adminAddr,
		authz.NewGenericAuthorization(sdk.MsgTypeURL(adminMsg)),
		&expiration,
	))

	exec := func(msg sdk.Msg) error {
		t.Helper()
		msgExec := authz.NewMsgExec(granteeAddr, []sdk.Msg{msg})
		// NewMsgExec formats with the SDK global prefix; exercise Guru's app address codec here.
		msgExec.Grantee = grantee
		_, err := authzKeeper.Exec(f.ctx, &msgExec)
		return err
	}

	granteeMsg := validRegisterExchangeMsg(grantee, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	err := exec(granteeMsg)
	require.ErrorIs(t, err, types.ErrAdminNotFound)
	_, err = f.keeper.GetExchange(f.ctx, 1)
	require.ErrorIs(t, err, types.ErrExchangeNotFound)

	require.NoError(t, exec(adminMsg))
	exchange, err := f.keeper.GetExchange(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, f.admin, exchange.GetAdminAddress())
}

func TestAuthzMsgExecUsesBexSignerFieldsForReserveAndFeeMessages(t *testing.T) {
	authzKey := storetypes.NewKVStoreKey(authzkeeper.StoreKey)
	f := setupKeeperFixtureWithExtraKVStoreKeys(t, map[string]*storetypes.KVStoreKey{authzkeeper.StoreKey: authzKey})
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	grantee, granteeAddr := testAddress(t, f.accountCodec, 0x19)
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, granteeAddr))

	authzKeeper := newBexAuthzKeeper(t, f, authzKey)
	expiration := f.ctx.BlockTime().Add(time.Hour)
	grant := func(msg sdk.Msg) {
		t.Helper()
		require.NoError(t, authzKeeper.SaveGrant(
			f.ctx,
			granteeAddr,
			f.adminAddr,
			authz.NewGenericAuthorization(sdk.MsgTypeURL(msg)),
			&expiration,
		))
	}
	exec := func(msg sdk.Msg) error {
		t.Helper()
		msgExec := authz.NewMsgExec(granteeAddr, []sdk.Msg{msg})
		// NewMsgExec formats with the SDK global prefix; exercise Guru's app address codec here.
		msgExec.Grantee = grantee
		_, err := authzKeeper.Exec(f.ctx, &msgExec)
		return err
	}

	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reserveAddr := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
	amount := []*basev1beta1.Coin{{Denom: "agxn", Amount: "9"}}

	deposit := &bexv1.MsgDepositReserve{Sender: f.admin, ExchangeId: exchange.GetId(), Amount: amount}
	grant(deposit)
	err := exec(&bexv1.MsgDepositReserve{Sender: grantee, ExchangeId: exchange.GetId(), Amount: amount})
	require.ErrorIs(t, err, types.ErrUnauthorizedReserveDepositor)
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).IsZero())

	require.NoError(t, exec(deposit))
	require.Equal(t, sdkmath.NewInt(9), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))
	require.NoError(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 4)))
	require.Equal(t, sdkmath.NewInt(5), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))

	inactive, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE},
	})
	require.NoError(t, err)
	recipient, err := f.accountCodec.BytesToString(f.recipient)
	require.NoError(t, err)

	withdrawReserve := &bexv1.MsgWithdrawReserve{AdminAddress: f.admin, ExchangeId: inactive.GetId(), Amount: []*basev1beta1.Coin{{Denom: "agxn", Amount: "2"}}, Recipient: recipient}
	grant(withdrawReserve)
	err = exec(&bexv1.MsgWithdrawReserve{AdminAddress: grantee, ExchangeId: inactive.GetId(), Amount: withdrawReserve.GetAmount(), Recipient: recipient})
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, f.recipient).IsZero())
	require.Equal(t, sdkmath.NewInt(5), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))

	require.NoError(t, exec(withdrawReserve))
	require.Equal(t, sdkmath.NewInt(2), f.bankKeeper.GetAllBalances(f.ctx, f.recipient).AmountOf("agxn"))
	require.Equal(t, sdkmath.NewInt(3), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).AmountOf("agxn"))

	withdrawFees := &bexv1.MsgWithdrawFees{AdminAddress: f.admin, ExchangeId: inactive.GetId(), Amount: []*basev1beta1.Coin{{Denom: "agxn", Amount: "3"}}, Recipient: recipient}
	grant(withdrawFees)
	err = exec(&bexv1.MsgWithdrawFees{AdminAddress: grantee, ExchangeId: inactive.GetId(), Amount: withdrawFees.GetAmount(), Recipient: recipient})
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	require.Equal(t, sdkmath.NewInt(2), f.bankKeeper.GetAllBalances(f.ctx, f.recipient).AmountOf("agxn"))
	collected, err := f.keeper.GetCollectedFees(f.ctx, inactive.GetId())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 4)), collected)

	require.NoError(t, exec(withdrawFees))
	require.Equal(t, sdkmath.NewInt(5), f.bankKeeper.GetAllBalances(f.ctx, f.recipient).AmountOf("agxn"))
	require.Equal(t, sdkmath.NewInt(1), f.bankKeeper.GetAllBalances(f.ctx, authtypes.NewModuleAddress(types.ModuleName)).AmountOf("agxn"))
	collected, err = f.keeper.GetCollectedFees(f.ctx, inactive.GetId())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)), collected)
}

func newBexAuthzKeeper(t *testing.T, f keeperTestFixture, authzKey *storetypes.KVStoreKey) authzkeeper.Keeper {
	t.Helper()

	encoding := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	types.RegisterInterfaces(encoding.InterfaceRegistry)
	authz.RegisterInterfaces(encoding.InterfaceRegistry)

	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(encoding.InterfaceRegistry)
	bexv1.RegisterMsgServer(router, NewMsgServer(&f.keeper))

	return authzkeeper.NewKeeper(
		runtime.NewKVStoreService(authzKey),
		encoding.Codec,
		router,
		f.accountKeeper,
	)
}

func TestKeeperGenesisExportImportAndInvariants(t *testing.T) {
	source := setupKeeperFixture(t)
	require.NoError(t, source.keeper.RegisterAdmin(source.ctx, source.moderator, source.admin))
	active := registerExchange(t, source, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	deleted := registerExchange(t, source, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, source.keeper.DeleteExchange(source.ctx, source.admin, deleted.GetId()))
	collectFee(t, source, active.GetId(), sdk.NewInt64Coin("agxn", 10))
	require.NoError(t, source.keeper.LockExchangeFee(source.ctx, active.GetId(), sdk.NewInt64Coin("agxn", 2)))
	require.NoError(t, source.keeper.RecordVolumeWindow(source.ctx, active.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(5)))

	genesis, err := source.keeper.ExportGenesis(source.ctx)
	require.NoError(t, err)
	require.Len(t, genesis.GetAdmins(), 1)
	require.Len(t, genesis.GetExchanges(), 2)
	require.NotZero(t, genesis.GetNextExchangeId())

	target := setupKeeperFixture(t)
	require.NoError(t, target.keeper.ImportGenesis(target.ctx, genesis))
	target.bankKeeper.SetBalance(authtypes.NewModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))
	exported, err := target.keeper.ExportGenesis(target.ctx)
	require.NoError(t, err)
	require.Len(t, exported.GetExchanges(), 2)
	require.Len(t, exported.GetVolumeWindows(), 1)
	target.bankKeeper.SetBalance(authtypes.NewModuleAddress(types.ModuleName), sdk.Coins{})
	require.ErrorIs(t, target.keeper.AssertInvariants(target.ctx), types.ErrInvariantViolation)
	target.bankKeeper.SetBalance(authtypes.NewModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))
	require.NoError(t, target.keeper.AssertInvariants(target.ctx))
	require.NoError(t, target.keeper.AssertFeeSolvency(target.ctx))
	reserveAddr := target.keeper.GetReserveAddress(target.ctx, active.GetId())
	delete(target.accountKeeper.accounts, string(reserveAddr))
	require.ErrorIs(t, target.keeper.AssertInvariants(target.ctx), types.ErrInvariantViolation)
	require.NoError(t, target.keeper.AssertFeeSolvency(target.ctx))
	target.accountKeeper.SetAccount(target.ctx, target.accountKeeper.NewAccountWithAddress(target.ctx, reserveAddr))
	nilAccountKeeper := target.keeper
	nilAccountKeeper.accountKeeper = nil
	require.ErrorIs(t, nilAccountKeeper.AssertInvariants(target.ctx), types.ErrInvariantViolation)
	require.NoError(t, target.keeper.AssertInvariants(target.ctx))
}

func bytesOf(b byte) []byte {
	out := make([]byte, 20)
	for i := range out {
		out[i] = b
	}
	return out
}

type storeFault struct {
	op     string
	prefix byte
	skip   int
	err    error
}

type faultStoreService struct {
	base  corestore.KVStoreService
	fault *storeFault
}

func (s faultStoreService) OpenKVStore(ctx context.Context) corestore.KVStore {
	return faultKVStore{KVStore: s.base.OpenKVStore(ctx), fault: s.fault}
}

type faultKVStore struct {
	corestore.KVStore
	fault *storeFault
}

func (s faultKVStore) fail(op string, key []byte) error {
	if s.fault == nil || s.fault.op != op {
		return nil
	}
	if s.fault.prefix != 0 && (len(key) == 0 || key[0] != s.fault.prefix) {
		return nil
	}
	if s.fault.skip > 0 {
		s.fault.skip--
		return nil
	}
	return s.fault.err
}

func (s faultKVStore) Get(key []byte) ([]byte, error) {
	if err := s.fail("get", key); err != nil {
		return nil, err
	}
	return s.KVStore.Get(key)
}

func (s faultKVStore) Has(key []byte) (bool, error) {
	if err := s.fail("has", key); err != nil {
		return false, err
	}
	return s.KVStore.Has(key)
}

func (s faultKVStore) Set(key, value []byte) error {
	if err := s.fail("set", key); err != nil {
		return err
	}
	return s.KVStore.Set(key, value)
}

func (s faultKVStore) Delete(key []byte) error {
	if err := s.fail("delete", key); err != nil {
		return err
	}
	return s.KVStore.Delete(key)
}

func (s faultKVStore) Iterator(start, end []byte) (corestore.Iterator, error) {
	if err := s.fail("iterator", start); err != nil {
		return nil, err
	}
	return s.KVStore.Iterator(start, end)
}

func (s faultKVStore) ReverseIterator(start, end []byte) (corestore.Iterator, error) {
	if err := s.fail("reverse_iterator", start); err != nil {
		return nil, err
	}
	return s.KVStore.ReverseIterator(start, end)
}
