package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestAdminLifecycleAndKeeperBasics(t *testing.T) {
	f := setupKeeperFixture(t)
	other, _ := testAddress(t, f.accountCodec, 0x04)

	require.Equal(t, f.moderator, f.constitutionKeeper.moderatorAddress)
	require.Equal(t, "bex/reserve/42", ReserveModuleName(42))
	require.NotNil(t, f.keeper.Logger(f.ctx))

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

	k := f.keeper
	k.bankKeeper = nil
	k.RegisterSendRestriction()
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
			name:    "bad port",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.PortA = "bad port" },
			wantErr: types.ErrInvalidRoute,
		},
		{
			name:    "bad channel",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.ChannelA = "channel" },
			wantErr: types.ErrInvalidRoute,
		},
		{
			name:    "missing active channel",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.ChannelA = "channel-404" },
			wantErr: types.ErrInvalidRoute,
		},
		{
			name: "closed active channel",
			mutate: func(msg *bexv1.MsgRegisterExchange) {
				f.channelKeeper.SetChannel("transfer", "channel-4", channeltypes.CLOSED)
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
			name:    "negative integer",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.LimitAToB = "-1" },
			wantErr: types.ErrInvalidRequest,
		},
		{
			name:    "integer too long",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.LimitAToB = strings.Repeat("9", maxIntDigits+1) },
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
			name:    "metadata empty key",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.Metadata = map[string]string{"": "v"} },
			wantErr: types.ErrInvalidRequest,
		},
		{
			name: "metadata key too long",
			mutate: func(msg *bexv1.MsgRegisterExchange) {
				msg.Metadata = map[string]string{strings.Repeat("k", maxMetadataKeyLen+1): "v"}
			},
			wantErr: types.ErrInvalidRequest,
		},
		{
			name: "metadata value too long",
			mutate: func(msg *bexv1.MsgRegisterExchange) {
				msg.Metadata = map[string]string{"k": strings.Repeat("v", maxMetadataValLen+1)}
			},
			wantErr: types.ErrInvalidRequest,
		},
		{
			name:    "zero epoch",
			mutate:  func(msg *bexv1.MsgRegisterExchange) { msg.VolumeEpochSeconds = 0 },
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

	nilChannelKeeper := f.keeper
	nilChannelKeeper.channelKeeper = nil
	_, err = nilChannelKeeper.RegisterExchange(f.ctx, validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE))
	require.ErrorIs(t, err, types.ErrInvalidRoute)
}

func TestUpdateExchangeFullPatchAndErrors(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	other, _ := testAddress(t, f.accountCodec, 0x05)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, other))

	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		DenomA:                            wrapperspb.String("agxn"),
		PortA:                             wrapperspb.String("transfer"),
		ChannelA:                          wrapperspb.String("channel-2"),
		DenomB:                            wrapperspb.String("gxusd"),
		PortB:                             wrapperspb.String("transfer"),
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
	f.channelKeeper.SetChannel("transfer", "channel-2", channeltypes.OPEN)
	f.channelKeeper.SetChannel("transfer", "channel-3", channeltypes.OPEN)
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

func TestFeesAccountingAndWithdrawals(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	collected, err := f.keeper.GetCollectedFees(f.ctx, 999)
	require.NoError(t, err)
	require.True(t, collected.IsZero())
	locked, err := f.keeper.GetLockedFees(f.ctx, 999)
	require.NoError(t, err)
	require.True(t, locked.IsZero())

	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 0)), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 0)), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.ReleaseExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 0)), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 0)), types.ErrInvalidRequest)

	collectFee(t, f, exchange.GetId(), sdk.NewInt64Coin("agxn", 100))
	require.ErrorIs(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 101)), types.ErrInsufficientAvailableFees)
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 40)))
	available, err := f.keeper.GetAvailableFees(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 60)), available)

	require.ErrorIs(t, f.keeper.ReleaseExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 41)), types.ErrInsufficientLockedFees)
	require.NoError(t, f.keeper.ReleaseExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 10)))
	require.ErrorIs(t, f.keeper.RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 31)), types.ErrInsufficientLockedFees)
	require.NoError(t, f.keeper.RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 20)))

	require.NoError(t, f.keeper.collectedFees.Set(f.ctx, 77, coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))))
	require.NoError(t, f.keeper.lockedFees.Set(f.ctx, 77, coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)))))
	_, err = f.keeper.GetAvailableFees(f.ctx, 77)
	require.ErrorIs(t, err, types.ErrInvariantViolation)

	require.ErrorIs(t, f.keeper.WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.Coins{}), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000))), types.ErrInsufficientAvailableFees)
	other, _ := testAddress(t, f.accountCodec, 0x06)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, other))
	require.ErrorIs(t, f.keeper.WithdrawFees(f.ctx, other, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrWrongExchangeAdmin)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	f.bankKeeper.SetBalance(moduleAddr, sdk.Coins{})
	require.Error(t, f.keeper.WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))))
	f.bankKeeper.SetBalance(moduleAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 100)))
	require.NoError(t, f.keeper.WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)), f.bankKeeper.GetAllBalances(f.ctx, f.recipient))
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
	require.NoError(t, f.keeper.RemoveAdmin(f.ctx, f.moderator, f.admin))

	requireEventTypes(
		t,
		f.ctx,
		types.EventTypeAdminRegistered,
		types.EventTypeAdminRemoved,
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
		types.EventTypeVolumeCapExceeded,
	)
}

func TestReserveDepositWithdrawAndRestrictions(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	active := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	inactive := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	to := sdk.AccAddress(bytesOf(0x08))
	rewritten, err := f.keeper.SendRestrictionFn(f.ctx, nil, to, nil)
	require.NoError(t, err)
	require.Equal(t, to, rewritten)

	reserveBytes, err := f.accountCodec.StringToBytes(inactive.GetReserveAddress())
	require.NoError(t, err)
	reserveAddr := sdk.AccAddress(reserveBytes)

	require.ErrorIs(t, f.keeper.DepositReserve(f.ctx, f.admin, active.GetId(), sdk.Coins{}), types.ErrInvalidRequest)
	require.Error(t, f.keeper.DepositReserve(f.ctx, f.admin, inactive.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 2_000_000))))
	require.NoError(t, f.keeper.DepositReserve(f.ctx, f.admin, inactive.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 25))))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 25)), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr))

	require.ErrorIs(t, f.keeper.WithdrawReserve(f.ctx, f.admin, active.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.WithdrawReserve(f.ctx, f.admin, inactive.GetId(), f.recipient, sdk.Coins{}), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.WithdrawReserve(f.ctx, f.admin, inactive.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 26))), types.ErrInsufficientReserve)
	require.NoError(t, f.keeper.WithdrawReserve(f.ctx, f.admin, inactive.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 5))))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 5)), f.bankKeeper.GetAllBalances(f.ctx, f.recipient))
	f.bankKeeper.restrictions = append(f.bankKeeper.restrictions, func(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) (sdk.AccAddress, error) {
		return nil, errors.New("send blocked")
	})
	require.Error(t, f.keeper.WithdrawReserve(f.ctx, f.admin, inactive.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))))
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

	require.Equal(t, sdkmath.ZeroInt(), ceilFee(sdkmath.NewInt(99), 0))
	maxAmount, ok := sdkmath.NewIntFromString(maxUint256String)
	require.True(t, ok)
	require.Equal(t, maxAmount, ceilFee(maxAmount, 9999).Add(maxAmount.QuoRaw(10000)))
	_, err = quoteAmountOut(sdkmath.LegacyNewDec(2), maxAmount)
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)
	tinyRate, err := sdkmath.LegacyNewDecFromStr("0.000000000000000001")
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewIntWithDecimal(1, 18), minAmountIn(tinyRate, 0))
	tooSmallRate, err := sdkmath.LegacyNewDecFromStr("0.000000000000000000")
	require.NoError(t, err)
	require.Equal(t, sdkmath.ZeroInt(), minAmountIn(tooSmallRate, 0))
}

func TestVolumeWindowAndQueries(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	ex1 := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	ex2 := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	ex3 := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, ex3.GetId()))
	collectFee(t, f, ex1.GetId(), sdk.NewInt64Coin("agxn", 10))
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, ex1.GetId(), sdk.NewInt64Coin("agxn", 3)))
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, ex1.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(7)))
	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Unix())

	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, ex1.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.ZeroInt()), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, ex1.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, sdkmath.NewInt(1)), types.ErrInvalidRoute)

	q := NewQueryServer(&f.keeper)
	for name, call := range map[string]func() error{
		"exchange":           func() error { _, err := q.Exchange(f.ctx, nil); return err },
		"exchanges":          func() error { _, err := q.Exchanges(f.ctx, nil); return err },
		"exchanges_by_admin": func() error { _, err := q.ExchangesByAdmin(f.ctx, nil); return err },
		"is_admin":           func() error { _, err := q.IsAdmin(f.ctx, nil); return err },
		"fees":               func() error { _, err := q.CollectedFees(f.ctx, nil); return err },
		"volume":             func() error { _, err := q.VolumeWindow(f.ctx, nil); return err },
		"quote":              func() error { _, err := q.QuoteSwap(f.ctx, nil); return err },
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

	listResp, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{Pagination: &queryv1beta1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, listResp.GetExchanges(), 1)
	require.NotEmpty(t, listResp.GetPagination().GetNextKey())
	listResp, err = q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{
		Pagination: &queryv1beta1.PageRequest{Key: listResp.GetPagination().GetNextKey(), Limit: 10000},
	})
	require.NoError(t, err)
	require.Len(t, listResp.GetExchanges(), 1)
	require.Equal(t, ex2.GetId(), listResp.GetExchanges()[0].GetId())
	listResp, err = q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{
		IncludeDeleted: true,
		Pagination:     &queryv1beta1.PageRequest{Limit: 10000},
	})
	require.NoError(t, err)
	require.Len(t, listResp.GetExchanges(), 3)
	invalidKeyPage, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{Pagination: &queryv1beta1.PageRequest{Key: []byte("bad")}})
	require.NoError(t, err)
	require.Empty(t, invalidKeyPage.GetExchanges())

	byAdmin, err := q.ExchangesByAdmin(f.ctx, &bexv1.QueryExchangesByAdminRequest{
		AdminAddress: f.admin,
		Pagination:   &queryv1beta1.PageRequest{Offset: 1, Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, byAdmin.GetExchanges(), 1)
	_, err = q.ExchangesByAdmin(f.ctx, &bexv1.QueryExchangesByAdminRequest{AdminAddress: "bad"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	isAdmin, err := q.IsAdmin(f.ctx, &bexv1.QueryIsAdminRequest{AdminAddress: f.admin})
	require.NoError(t, err)
	require.True(t, isAdmin.GetIsAdmin())
	_, err = q.IsAdmin(f.ctx, &bexv1.QueryIsAdminRequest{AdminAddress: "bad"})
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
	badVolumeKey := currentVolumeKey(f.ctx.BlockTime(), ex1.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, ex1.GetVolumeEpochSeconds())
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, badVolumeKey, "bad"))
	_, err = f.keeper.GetCurrentVolumeAmount(f.ctx, ex1, bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	quote, err := q.QuoteSwap(f.ctx, &bexv1.QueryQuoteSwapRequest{ExchangeId: ex1.GetId(), InputDenom: ex1.GetDenomA(), AmountIn: "2"})
	require.NoError(t, err)
	require.Equal(t, "2", quote.GetQuote().GetAmountOut())
}

func TestExchangeQueryFiltersTombstonesFromPrimaryState(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	first := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	deleted := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	last := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, deleted.GetId()))

	q := NewQueryServer(&f.keeper)
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

func TestVolumePendingEpochActivationAndPrune(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B

	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), direction, sdkmath.NewInt(10)))
	used, err := f.keeper.GetCurrentVolumeAmount(f.ctx, exchange, direction)
	require.NoError(t, err)
	require.Equal(t, "10", used.String())

	effectiveAt := uint64(f.ctx.BlockTime().Add(time.Second).Unix())
	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 2),
		PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(effectiveAt),
	})
	require.NoError(t, err)

	used, err = f.keeper.GetCurrentVolumeAmount(f.ctx, updated, direction)
	require.NoError(t, err)
	require.Equal(t, "10", used.String())

	futureCtx := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(2 * time.Second))
	used, err = f.keeper.GetCurrentVolumeAmount(futureCtx, updated, direction)
	require.NoError(t, err)
	require.True(t, used.IsZero())

	require.NoError(t, f.keeper.RecordVolumeWindow(futureCtx, exchange.GetId(), direction, sdkmath.NewInt(5)))
	persisted, err := f.keeper.GetExchange(futureCtx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, minVolumeEpochSecs*2, persisted.GetVolumeEpochSeconds())
	require.Zero(t, persisted.GetPendingVolumeEpochSeconds())
	require.Zero(t, persisted.GetPendingVolumeEpochEffectiveAtUnix())
	require.Equal(t, updated.GetRevision()+1, persisted.GetRevision())

	newKey := currentVolumeKey(futureCtx.BlockTime(), exchange.GetId(), direction, minVolumeEpochSecs*2)
	value, err := f.keeper.volumeWindow.Get(futureCtx, newKey)
	require.NoError(t, err)
	require.Equal(t, "5", value)

	pruneExchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	expiredStart := uint64(f.ctx.BlockTime().Unix()) - 2*uint64(minVolumeEpochSecs)
	expiredKey := collections.Join4(pruneExchange.GetId(), uint32(direction), expiredStart, minVolumeEpochSecs)
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, expiredKey, "1"))
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, pruneExchange.GetId(), direction, sdkmath.NewInt(1)))
	_, err = f.keeper.volumeWindow.Get(f.ctx, expiredKey)
	require.ErrorIs(t, err, collections.ErrNotFound)
	cursor, err := f.keeper.volumePruneCursor.Get(f.ctx, collections.Join(pruneExchange.GetId(), uint32(direction)))
	require.NoError(t, err)
	require.NotZero(t, cursor)

	steadyExchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, steadyExchange.GetId(), direction, sdkmath.NewInt(1)))
	steadyExpiredKey := collections.Join4(steadyExchange.GetId(), uint32(direction), uint64(1), uint32(1))
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, steadyExpiredKey, "1"))
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, steadyExchange.GetId(), direction, sdkmath.NewInt(1)))
	value, err = f.keeper.volumeWindow.Get(f.ctx, steadyExpiredKey)
	require.NoError(t, err)
	require.Equal(t, "1", value)

	rolloverCtx := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(minVolumeEpochSecs) * time.Second))
	require.NoError(t, f.keeper.RecordVolumeWindow(rolloverCtx, steadyExchange.GetId(), direction, sdkmath.NewInt(1)))
	_, err = f.keeper.volumeWindow.Get(rolloverCtx, steadyExpiredKey)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestVolumeWindowPruneAndEpochEdgeCases(t *testing.T) {
	f := setupKeeperFixture(t)
	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B
	now := uint64(f.ctx.BlockTime().Unix())

	exchange := &bexv1.Exchange{
		VolumeEpochSeconds:                minVolumeEpochSecs,
		PendingVolumeEpochSeconds:         minVolumeEpochSecs * 2,
		PendingVolumeEpochEffectiveAtUnix: 1,
	}
	require.Equal(t, minVolumeEpochSecs, effectiveVolumeEpochSeconds(exchange, time.Time{}))
	require.Equal(t, minVolumeEpochSecs, effectiveVolumeEpochSeconds(exchange, time.Unix(-1, 0)))
	require.Equal(t, minVolumeEpochSecs*2, effectiveVolumeEpochSeconds(exchange, time.Unix(1, 0)))
	require.False(t, volumeWindowExpired(collections.Join4(uint64(1), uint32(direction), now+1, minVolumeEpochSecs), now))
	require.False(t, volumeWindowExpired(collections.Join4(uint64(1), uint32(direction), uint64(1), uint32(0)), now))

	require.NoError(t, f.keeper.pruneExpiredVolumeWindows(f.ctx, 700, direction, 0))
	require.NoError(t, f.keeper.pruneExpiredVolumeWindows(f.ctx.WithBlockTime(time.Time{}), 700, direction, maxVolumePruneRowsPerRecord))

	skipID := uint64(701)
	skipKey := collections.Join4(skipID, uint32(direction), uint64(1), uint32(1))
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, skipKey, "1"))
	require.NoError(t, f.keeper.volumePruneCursor.Set(f.ctx, collections.Join(skipID, uint32(direction)), 2))
	require.NoError(t, f.keeper.pruneExpiredVolumeWindows(f.ctx, skipID, direction, maxVolumePruneRowsPerRecord))
	_, err := f.keeper.volumeWindow.Get(f.ctx, skipKey)
	require.NoError(t, err)

	limitedID := uint64(702)
	firstKey := collections.Join4(limitedID, uint32(direction), uint64(1), uint32(1))
	secondKey := collections.Join4(limitedID, uint32(direction), uint64(2), uint32(1))
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, firstKey, "1"))
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, secondKey, "1"))
	require.NoError(t, f.keeper.pruneExpiredVolumeWindows(f.ctx, limitedID, direction, 1))
	_, err = f.keeper.volumeWindow.Get(f.ctx, firstKey)
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.volumeWindow.Get(f.ctx, secondKey)
	require.NoError(t, err)
}

func TestMsgServerRoutes(t *testing.T) {
	f := setupKeeperFixture(t)
	msgServer := NewMsgServer(&f.keeper)
	coin := []*basev1beta1.Coin{{Denom: "agxn", Amount: "10"}}

	nilCalls := []func() error{
		func() error { _, err := msgServer.RegisterAdmin(f.ctx, nil); return err },
		func() error { _, err := msgServer.RemoveAdmin(f.ctx, nil); return err },
		func() error { _, err := msgServer.RegisterExchange(f.ctx, nil); return err },
		func() error { _, err := msgServer.UpdateExchange(f.ctx, nil); return err },
		func() error { _, err := msgServer.DeleteExchange(f.ctx, nil); return err },
		func() error { _, err := msgServer.DepositReserve(f.ctx, nil); return err },
		func() error { _, err := msgServer.WithdrawReserve(f.ctx, nil); return err },
		func() error { _, err := msgServer.WithdrawFees(f.ctx, nil); return err },
	}
	for _, call := range nilCalls {
		require.ErrorIs(t, call(), types.ErrInvalidRequest)
	}

	_, err := msgServer.RegisterAdmin(f.ctx, &bexv1.MsgRegisterAdmin{Moderator: f.moderator, AdminAddress: f.admin})
	require.NoError(t, err)
	regResp, err := msgServer.RegisterExchange(f.ctx, validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE))
	require.NoError(t, err)
	require.NotZero(t, regResp.GetExchangeId())
	require.NotEmpty(t, regResp.GetReserveAddress())

	_, err = msgServer.UpdateExchange(f.ctx, &bexv1.MsgUpdateExchange{
		AdminAddress:     f.admin,
		ExchangeId:       regResp.GetExchangeId(),
		ExpectedRevision: 1,
		Patch:            &bexv1.ExchangeUpdatePatch{FeeBpsAToB: wrapperspb.UInt32(7)},
	})
	require.NoError(t, err)
	_, err = msgServer.DepositReserve(f.ctx, &bexv1.MsgDepositReserve{Sender: f.admin, ExchangeId: regResp.GetExchangeId(), Amount: coin})
	require.NoError(t, err)
	recipient, err := f.accountCodec.BytesToString(f.recipient)
	require.NoError(t, err)
	_, err = msgServer.WithdrawReserve(f.ctx, &bexv1.MsgWithdrawReserve{AdminAddress: f.admin, ExchangeId: regResp.GetExchangeId(), Amount: coin, Recipient: recipient})
	require.NoError(t, err)

	current, err := f.keeper.GetExchange(f.ctx, regResp.GetExchangeId())
	require.NoError(t, err)
	current, err = f.keeper.UpdateExchange(f.ctx, f.admin, current.GetId(), current.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE},
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.DepositReserve(f.ctx, f.admin, current.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 5))))
	require.NoError(t, f.keeper.CollectFee(f.ctx, current.GetId(), sdk.NewInt64Coin("agxn", 5)))
	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, current.GetId(), current.GetRevision(), &bexv1.ExchangeUpdatePatch{
		Status: &bexv1.ExchangeStatusPatch{Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE},
	})
	require.NoError(t, err)
	_, err = msgServer.WithdrawFees(f.ctx, &bexv1.MsgWithdrawFees{AdminAddress: f.admin, ExchangeId: regResp.GetExchangeId(), Amount: []*basev1beta1.Coin{{Denom: "agxn", Amount: "5"}}, Recipient: recipient})
	require.NoError(t, err)
	_, err = msgServer.DeleteExchange(f.ctx, &bexv1.MsgDeleteExchange{AdminAddress: f.admin, ExchangeId: regResp.GetExchangeId()})
	require.NoError(t, err)
	_, err = msgServer.RemoveAdmin(f.ctx, &bexv1.MsgRemoveAdmin{Moderator: f.moderator, AdminAddress: f.admin})
	require.NoError(t, err)

	_, err = msgServer.DepositReserve(f.ctx, &bexv1.MsgDepositReserve{Sender: f.admin, ExchangeId: regResp.GetExchangeId(), Amount: []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}}})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = msgServer.WithdrawReserve(f.ctx, &bexv1.MsgWithdrawReserve{AdminAddress: f.admin, ExchangeId: regResp.GetExchangeId(), Amount: coin, Recipient: "bad"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = msgServer.WithdrawFees(f.ctx, &bexv1.MsgWithdrawFees{AdminAddress: f.admin, ExchangeId: regResp.GetExchangeId(), Amount: coin, Recipient: "bad"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

func TestKeeperGenesisExportImportAndInvariants(t *testing.T) {
	source := setupKeeperFixture(t)
	require.NoError(t, source.keeper.RegisterAdmin(source.ctx, source.moderator, source.admin))
	require.NoError(t, ValidateVolumeEpochForGenesis("volume_epoch_seconds", minVolumeEpochSecs, false))
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

	require.Error(t, target.keeper.ImportGenesis(target.ctx, &bexv1.GenesisState{Admins: []string{"bad"}}))
	require.NoError(t, target.keeper.setNextExchangeID(target.ctx, 0))
	next, err := target.keeper.nextExchangeID.Peek(target.ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultNextExchangeID, next)

	canonical, addr, err := target.keeper.CanonicalAddressForGenesis(target.admin)
	require.NoError(t, err)
	require.NotEmpty(t, canonical)
	require.NotEmpty(t, addr)
	require.NoError(t, ValidateExchangeForGenesis(active))
	require.NoError(t, ValidateExchangeForGenesis(&bexv1.Exchange{Id: 99, Status: bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED}))
	coins, err := ProtoCoinsForGenesis([]*basev1beta1.Coin{{Denom: "agxn", Amount: "1"}})
	require.NoError(t, err)
	require.True(t, HasCoinsForGenesis(coins, coins))
	_, err = ParseIntForGenesis("value", "1")
	require.NoError(t, err)
}

func TestValidationHelpers(t *testing.T) {
	require.ErrorIs(t, validateExchangeConfig(nil), types.ErrInvalidRequest)
	require.ErrorIs(t, validateExchangeConfig(&bexv1.Exchange{}), types.ErrInvalidRequest)
	require.ErrorIs(t, validateStatus(bexv1.ExchangeStatus_EXCHANGE_STATUS_UNSPECIFIED), types.ErrInvalidRequest)

	_, err := buildIBCDenom("agxn", "transfer", "channel-0")
	require.NoError(t, err)
	_, err = protoCoinsToSDK([]*basev1beta1.Coin{nil})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = protoCoinsToSDK([]*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = protoCoinsToSDK([]*basev1beta1.Coin{{Denom: "agxn", Amount: "0"}})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	coins, err := ledgerToCoins(nil)
	require.NoError(t, err)
	require.True(t, coins.IsZero())
	coins, err = ledgerToCoins(&bexv1.FeeLedger{})
	require.NoError(t, err)
	require.True(t, coins.IsZero())
	_, err = ledgerToCoins(&bexv1.FeeLedger{Coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}}})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	require.Nil(t, sortedMetadataCopy(nil))
	require.Equal(t, map[string]string{"a": "1"}, sortedMetadataCopy(map[string]string{"a": "1"}))
	require.True(t, hasCoins(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)), sdk.Coins{}))
	require.False(t, hasCoins(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)), sdk.NewCoins(sdk.NewInt64Coin("agxn", 2))))

}

func TestAdditionalErrorBranches(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	active := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	inactive := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	other, _ := testAddress(t, f.accountCodec, 0x09)

	_, _, err := f.keeper.requireRegisteredAdmin(f.ctx, "bad")
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, _, err = f.keeper.requireExchangeAdmin(f.ctx, active, other)
	require.ErrorIs(t, err, types.ErrAdminNotFound)
	require.ErrorIs(t, f.keeper.DepositReserve(f.ctx, other, inactive.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrUnauthorizedReserveDepositor)
	require.ErrorIs(t, f.keeper.DepositReserve(f.ctx, f.admin, 999, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrExchangeNotFound)
	require.ErrorIs(t, f.keeper.WithdrawReserve(f.ctx, f.admin, 999, f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrExchangeNotFound)
	require.ErrorIs(t, f.keeper.WithdrawReserve(f.ctx, other, inactive.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrAdminNotFound)
	require.ErrorIs(t, f.keeper.DeleteExchange(f.ctx, f.admin, 999), types.ErrExchangeNotFound)
	require.ErrorIs(t, f.keeper.DeleteExchange(f.ctx, other, inactive.GetId()), types.ErrAdminNotFound)
	require.ErrorIs(t, f.keeper.DeleteExchange(f.ctx, f.admin, active.GetId()), types.ErrInvalidRequest)
	require.ErrorIs(t, f.keeper.CollectFee(f.ctx, 999, sdk.NewInt64Coin("agxn", 1)), types.ErrExchangeNotFound)
	require.ErrorIs(t, f.keeper.LockExchangeFee(f.ctx, 999, sdk.NewInt64Coin("agxn", 1)), types.ErrExchangeNotFound)
	require.ErrorIs(t, f.keeper.RefundLockedFee(f.ctx, 999, sdk.NewInt64Coin("agxn", 1)), types.ErrExchangeNotFound)
	require.ErrorIs(t, f.keeper.WithdrawFees(f.ctx, f.admin, 999, f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrExchangeNotFound)
	msgB := validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	msgB.DenomB = "bad denom"
	_, err = f.keeper.RegisterExchange(f.ctx, msgB)
	require.ErrorIs(t, err, types.ErrInvalidRoute)

	_, err = f.keeper.ResolveSwapDirection(f.ctx, 999, "agxn")
	require.ErrorIs(t, err, types.ErrExchangeNotFound)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "bad"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)
	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, 999, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), types.ErrExchangeNotFound)

	capped := cloneExchange(active)
	capped.VolumeCapAToB = "1"
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, capped.GetId(), capped))
	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, capped.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(2)), types.ErrVolumeCapExceeded)
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, active.GetId(), active))
	badStored := cloneExchange(active)
	badStored.LimitAToB = "bad"
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, badStored.GetId(), badStored))
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: badStored.GetId(), InputDenom: badStored.GetDenomA(), AmountIn: "1"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, active.GetId(), active))
	badVolumeKey := currentVolumeKey(f.ctx.BlockTime(), active.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, active.GetVolumeEpochSeconds())
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, badVolumeKey, "bad"))
	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "2"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	require.NoError(t, f.keeper.volumeWindow.Remove(f.ctx, badVolumeKey))

	for _, mutate := range []func(*bexv1.Exchange){
		func(e *bexv1.Exchange) { e.LimitAToB = "bad" },
		func(e *bexv1.Exchange) { e.VolumeCapAToB = "bad" },
		func(e *bexv1.Exchange) { e.LimitBToA = "bad" },
		func(e *bexv1.Exchange) { e.VolumeCapBToA = "bad" },
	} {
		exchange := cloneExchange(active)
		mutate(exchange)
		_, _, _, _, _, err = quoteConfig(exchange, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B)
		if exchange.GetLimitBToA() == "bad" || exchange.GetVolumeCapBToA() == "bad" {
			_, _, _, _, _, err = quoteConfig(exchange, bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A)
		}
		require.ErrorIs(t, err, types.ErrInvalidRequest)
	}

	require.Equal(t, uint64(0), currentVolumeKey(time.Time{}, 1, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, 0).K3())
	_, err = validateIntString("value", "")
	require.NoError(t, err)
	amount, err := validateIntString("value", maxUint256String)
	require.NoError(t, err)
	require.Equal(t, maxUint256String, amount.String())
	_, err = validateIntString("value", "115792089237316195423570985008687907853269984665640564039457584007913129639936")
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = validateIntString("value", "0x10")
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = validateIntString("value", strings.Repeat("9", maxIntDigits))
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	digits, negative, decimal := decimalDigits("")
	require.False(t, decimal)
	require.False(t, negative)
	require.Empty(t, digits)
	digits, negative, decimal = decimalDigits("+7")
	require.True(t, decimal)
	require.False(t, negative)
	require.Equal(t, "7", digits)
	digits, negative, decimal = decimalDigits("+")
	require.False(t, decimal)
	require.False(t, negative)
	require.Empty(t, digits)
	exchange := cloneExchange(active)
	exchange.Status = bexv1.ExchangeStatus_EXCHANGE_STATUS_UNSPECIFIED
	require.ErrorIs(t, validateExchangeConfig(exchange), types.ErrInvalidRequest)
	require.ErrorIs(t, validateMutableExchangeConfig(nil), types.ErrInvalidRequest)
	exchange = cloneExchange(active)
	exchange.DenomA = "bad denom"
	require.ErrorIs(t, validateExchangeConfig(exchange), types.ErrInvalidRoute)
	exchange = cloneExchange(active)
	exchange.DenomB = "bad denom"
	require.ErrorIs(t, validateExchangeConfig(exchange), types.ErrInvalidRoute)
	exchange = cloneExchange(active)
	exchange.FeeBpsBToA = 10000
	require.ErrorIs(t, validateExchangeConfig(exchange), types.ErrInvalidFeeBps)
	exchange = cloneExchange(active)
	exchange.PendingVolumeEpochSeconds = minVolumeEpochSecs - 1
	exchange.PendingVolumeEpochEffectiveAtUnix = uint64(f.ctx.BlockTime().Unix())
	require.ErrorIs(t, validateExchangeConfig(exchange), types.ErrInvalidRequest)
	exchange = cloneExchange(active)
	exchange.PendingVolumeEpochEffectiveAtUnix = uint64(f.ctx.BlockTime().Unix())
	require.ErrorIs(t, validateExchangeConfig(exchange), types.ErrInvalidRequest)
	exchange = cloneExchange(active)
	exchange.PendingVolumeEpochSeconds = minVolumeEpochSecs
	require.ErrorIs(t, validateExchangeConfig(exchange), types.ErrInvalidRequest)
	_, err = buildIBCDenom("agxn", "transfer", "channel-0")
	require.NoError(t, err)

	badCodecKeeper := f.keeper
	badCodecKeeper.accountCodec = failingAddressCodec{base: f.accountCodec, failBytesToString: true}
	_, _, err = badCodecKeeper.canonicalAddress(f.admin)
	require.Error(t, err)
	_, err = badCodecKeeper.SendRestrictionFn(f.ctx, nil, f.recipient, nil)
	require.Error(t, err)

	reserveFailKeeper := f.keeper
	reserveFailKeeper.accountCodec = failingAddressCodec{base: f.accountCodec, failStringValue: inactive.GetReserveAddress()}
	require.ErrorIs(t, reserveFailKeeper.DepositReserve(f.ctx, f.admin, inactive.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrInvalidRoute)
	require.ErrorIs(t, reserveFailKeeper.WithdrawReserve(f.ctx, f.admin, inactive.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), types.ErrInvalidRoute)
	require.ErrorIs(t, reserveFailKeeper.DeleteExchange(f.ctx, f.admin, inactive.GetId()), types.ErrInvalidRoute)

	recipientFailKeeper := f.keeper
	recipientFailKeeper.accountCodec = failingAddressCodec{base: f.accountCodec, failBytesValue: f.recipient}
	inactiveReserveBytes, err := f.accountCodec.StringToBytes(inactive.GetReserveAddress())
	require.NoError(t, err)
	f.bankKeeper.SetBalance(sdk.AccAddress(inactiveReserveBytes), sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)))
	require.Error(t, recipientFailKeeper.WithdrawReserve(f.ctx, f.admin, inactive.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))))
	collectFee(t, f, active.GetId(), sdk.NewInt64Coin("agxn", 2))
	require.Error(t, recipientFailKeeper.WithdrawFees(f.ctx, f.admin, active.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))))

	moduleCodecKeeper := f.keeper
	nextExchangeID, err := f.keeper.nextExchangeID.Peek(f.ctx)
	require.NoError(t, err)
	moduleCodecKeeper.accountCodec = failingAddressCodec{base: f.accountCodec, failBytesValue: f.keeper.GetReserveAddress(f.ctx, nextExchangeID)}
	_, err = moduleCodecKeeper.RegisterExchange(f.ctx, validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE))
	require.Error(t, err)
	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, inactive.GetId(), inactive.GetRevision(), &bexv1.ExchangeUpdatePatch{
		DenomB: wrapperspb.String("bad denom"),
	})
	require.ErrorIs(t, err, types.ErrInvalidRoute)
	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, inactive.GetId(), inactive.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsBToA: wrapperspb.UInt32(10000),
	})
	require.ErrorIs(t, err, types.ErrInvalidFeeBps)

	zeroCtx := f.ctx.WithBlockTime(time.Time{})
	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", 1)
	_, err = f.keeper.QuoteSwap(zeroCtx, &bexv1.QuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "2"})
	require.ErrorIs(t, err, types.ErrStaleOracleRate)

	q := NewQueryServer(&f.keeper)
	deletedForList := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, deletedForList.GetId()))
	list, err := q.Exchanges(f.ctx, &bexv1.QueryExchangesRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, list.GetExchanges())
	for _, exchange := range list.GetExchanges() {
		require.NotEqual(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED, exchange.GetStatus())
	}
	invalidAdminKeyPage, err := q.ExchangesByAdmin(f.ctx, &bexv1.QueryExchangesByAdminRequest{
		AdminAddress: f.admin,
		Pagination:   &queryv1beta1.PageRequest{Key: []byte("bad")},
	})
	require.NoError(t, err)
	require.Empty(t, invalidAdminKeyPage.GetExchanges())
	limited, err := q.ExchangesByAdmin(f.ctx, &bexv1.QueryExchangesByAdminRequest{
		AdminAddress: f.admin,
		Pagination:   &queryv1beta1.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, limited.GetExchanges(), 1)
	require.NoError(t, f.keeper.exchanges.Remove(f.ctx, inactive.GetId()))
	_, err = q.ExchangesByAdmin(f.ctx, &bexv1.QueryExchangesByAdminRequest{AdminAddress: f.admin})
	require.ErrorIs(t, err, types.ErrExchangeNotFound)
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, inactive.GetId(), inactive))
	require.NoError(t, f.keeper.collectedFees.Set(f.ctx, active.GetId(), coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))))
	require.NoError(t, f.keeper.lockedFees.Set(f.ctx, active.GetId(), coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)))))
	_, err = q.AvailableFees(f.ctx, &bexv1.QueryFeesRequest{ExchangeId: active.GetId()})
	require.ErrorIs(t, err, types.ErrInvariantViolation)
	require.NoError(t, f.keeper.lockedFees.Set(f.ctx, active.GetId(), coinsToLedger(sdk.Coins{})))
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, badVolumeKey, "bad"))
	_, err = q.VolumeWindow(f.ctx, &bexv1.QueryVolumeWindowRequest{ExchangeId: active.GetId(), Direction: bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	require.NoError(t, f.keeper.volumeWindow.Remove(f.ctx, badVolumeKey))
	_, err = q.VolumeWindow(f.ctx, &bexv1.QueryVolumeWindowRequest{ExchangeId: 999, Direction: bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B})
	require.ErrorIs(t, err, types.ErrExchangeNotFound)
	_, err = q.QuoteSwap(f.ctx, &bexv1.QueryQuoteSwapRequest{ExchangeId: active.GetId(), InputDenom: active.GetDenomA(), AmountIn: "bad"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	msgServer := NewMsgServer(&f.keeper)
	_, err = msgServer.RegisterExchange(f.ctx, validRegisterExchangeMsg(other, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE))
	require.ErrorIs(t, err, types.ErrAdminNotFound)
	_, err = msgServer.UpdateExchange(f.ctx, &bexv1.MsgUpdateExchange{
		AdminAddress:     f.admin,
		ExchangeId:       inactive.GetId(),
		ExpectedRevision: 99,
		Patch:            &bexv1.ExchangeUpdatePatch{FeeBpsAToB: wrapperspb.UInt32(1)},
	})
	require.ErrorIs(t, err, types.ErrRevisionConflict)
	_, err = msgServer.WithdrawReserve(f.ctx, &bexv1.MsgWithdrawReserve{
		AdminAddress: f.admin,
		ExchangeId:   inactive.GetId(),
		Amount:       []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}},
		Recipient:    f.admin,
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = msgServer.WithdrawFees(f.ctx, &bexv1.MsgWithdrawFees{
		AdminAddress: f.admin,
		ExchangeId:   inactive.GetId(),
		Amount:       []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}},
		Recipient:    f.admin,
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = msgServer.WithdrawFees(f.ctx, &bexv1.MsgWithdrawFees{
		AdminAddress: f.admin,
		ExchangeId:   inactive.GetId(),
		Amount:       []*basev1beta1.Coin{{Denom: "agxn", Amount: "1"}},
		Recipient:    f.admin,
	})
	require.ErrorIs(t, err, types.ErrInsufficientAvailableFees)

	require.Error(t, f.keeper.ImportGenesis(f.ctx, &bexv1.GenesisState{
		CollectedFees: []*bexv1.FeeGenesis{{ExchangeId: active.GetId(), Coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}}}},
	}))
	require.Error(t, f.keeper.ImportGenesis(f.ctx, &bexv1.GenesisState{
		LockedFees: []*bexv1.FeeGenesis{{ExchangeId: active.GetId(), Coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}}}},
	}))
	require.ErrorIs(t, f.keeper.ImportGenesis(f.ctx, &bexv1.GenesisState{
		VolumeWindows: []*bexv1.VolumeWindowGenesis{{ExchangeId: active.GetId(), Direction: bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, EpochSeconds: minVolumeEpochSecs, Amount: "bad"}},
	}), types.ErrInvalidRequest)
	fresh := setupKeeperFixture(t)
	exported, err := fresh.keeper.ExportGenesis(fresh.ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultNextExchangeID, exported.GetNextExchangeId())
	require.NoError(t, f.keeper.collectedFees.Set(f.ctx, active.GetId(), &bexv1.FeeLedger{Coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "bad"}}}))
	require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvalidRequest)
	require.NoError(t, f.keeper.collectedFees.Set(f.ctx, active.GetId(), coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))))
	require.NoError(t, f.keeper.lockedFees.Set(f.ctx, active.GetId(), coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)))))
	require.ErrorIs(t, f.keeper.AssertInvariants(f.ctx), types.ErrInvariantViolation)
}

func TestStoreFaultBranches(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	deleteExchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.AddReserveDepositor(f.ctx, f.admin, exchange.GetId(), f.admin))
	collectFee(t, f, exchange.GetId(), sdk.NewInt64Coin("agxn", 3))
	require.NoError(t, f.keeper.LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)))
	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)))
	reserveAddr := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))

	faultErr := errors.New("store fault")
	faulty := func(op string, prefix byte) Keeper {
		return NewKeeper(
			faultStoreService{base: f.storeService, fault: &storeFault{op: op, prefix: prefix, err: faultErr}},
			f.accountCodec,
			f.accountKeeper,
			f.bankKeeper,
			f.oracleKeeper,
			f.constitutionKeeper,
			f.channelKeeper,
		)
	}
	faultySkip := func(op string, prefix byte, skip int) Keeper {
		return NewKeeper(
			faultStoreService{base: f.storeService, fault: &storeFault{op: op, prefix: prefix, skip: skip, err: faultErr}},
			f.accountCodec,
			f.accountKeeper,
			f.bankKeeper,
			f.oracleKeeper,
			f.constitutionKeeper,
			f.channelKeeper,
		)
	}
	genesis, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	require.ErrorIs(t, faulty("has", 0x01).RegisterAdmin(f.ctx, f.moderator, f.admin), faultErr)
	require.ErrorIs(t, faulty("has", 0x01).RemoveAdmin(f.ctx, f.moderator, f.admin), faultErr)
	otherAdmin, _ := testAddress(t, f.accountCodec, 0x16)
	require.ErrorIs(t, faulty("set", 0x01).RegisterAdmin(f.ctx, f.moderator, otherAdmin), faultErr)
	require.ErrorIs(t, faulty("delete", 0x01).RemoveAdmin(f.ctx, f.moderator, f.admin), faultErr)
	_, err = faulty("has", 0x01).IsAdmin(f.ctx, f.admin)
	require.ErrorIs(t, err, faultErr)
	_, err = faulty("get", 0x02).GetExchange(f.ctx, exchange.GetId())
	require.ErrorIs(t, err, faultErr)
	_, err = faulty("get", 0x06).GetAvailableFees(f.ctx, exchange.GetId())
	require.ErrorIs(t, err, faultErr)
	_, err = faulty("get", 0x07).GetAvailableFees(f.ctx, exchange.GetId())
	require.ErrorIs(t, err, faultErr)
	require.ErrorIs(t, faulty("get", 0x06).CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("get", 0x06).LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("get", 0x07).LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("get", 0x07).ReleaseExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("get", 0x06).RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("get", 0x07).RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("get", 0x06).WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), faultErr)
	require.ErrorIs(t, faultySkip("get", 0x06, 1).WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), faultErr)
	require.ErrorIs(t, faulty("has", 0x01).DepositReserve(f.ctx, f.admin, exchange.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), faultErr)
	_, err = faulty("get", 0x08).GetCurrentVolumeAmount(f.ctx, exchange, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B)
	require.ErrorIs(t, err, faultErr)
	pendingExchange := cloneExchange(exchange)
	pendingExchange.PendingVolumeEpochSeconds = minVolumeEpochSecs * 2
	pendingExchange.PendingVolumeEpochEffectiveAtUnix = uint64(f.ctx.BlockTime().Unix())
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, pendingExchange.GetId(), pendingExchange))
	require.ErrorIs(t, faulty("set", 0x02).RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), faultErr)
	require.NoError(t, f.keeper.exchanges.Set(f.ctx, exchange.GetId(), exchange))
	rolloverCtx := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(minVolumeEpochSecs) * time.Second))
	require.ErrorIs(t, faulty("get", 0x09).RecordVolumeWindow(rolloverCtx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), faultErr)
	require.ErrorIs(t, faulty("iterator", 0x08).RecordVolumeWindow(rolloverCtx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), faultErr)
	expiredKey := collections.Join4(exchange.GetId(), uint32(bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B), uint64(1), uint32(1))
	require.NoError(t, f.keeper.volumeWindow.Set(f.ctx, expiredKey, "1"))
	require.ErrorIs(t, faulty("delete", 0x08).RecordVolumeWindow(rolloverCtx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), faultErr)
	require.ErrorIs(t, faulty("set", 0x09).RecordVolumeWindow(rolloverCtx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), faultErr)
	require.ErrorIs(t, faulty("get", 0x08).RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), faultErr)
	_, err = faulty("get", 0x05).RegisterExchange(f.ctx, validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE))
	require.ErrorIs(t, err, faultErr)
	_, err = faulty("get", 0x05).nextID(f.ctx)
	require.ErrorIs(t, err, faultErr)
	require.ErrorIs(t, faulty("get", 0x05).ensureNextExchangeID(f.ctx), faultErr)

	for _, prefix := range []byte{0x02, 0x03, 0x04, 0x06, 0x07} {
		_, err = faulty("set", prefix).RegisterExchange(f.ctx, validRegisterExchangeMsg(f.admin, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE))
		require.ErrorIs(t, err, faultErr)
	}

	_, err = faulty("set", 0x02).UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(9),
	})
	require.ErrorIs(t, err, faultErr)
	require.ErrorIs(t, faulty("set", 0x02).DeleteExchange(f.ctx, f.admin, deleteExchange.GetId()), faultErr)
	require.ErrorIs(t, faulty("get", 0x06).DeleteExchange(f.ctx, f.admin, deleteExchange.GetId()), faultErr)
	require.ErrorIs(t, faulty("get", 0x07).DeleteExchange(f.ctx, f.admin, deleteExchange.GetId()), faultErr)
	require.ErrorIs(t, faulty("set", 0x06).CollectFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("set", 0x07).LockExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("set", 0x07).ReleaseExchangeFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("set", 0x06).RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	require.ErrorIs(t, faulty("set", 0x07).RefundLockedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)), faultErr)
	f.bankKeeper.SetBalance(authtypes.NewModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewInt64Coin("agxn", 3)))
	require.ErrorIs(t, faulty("set", 0x06).WithdrawFees(f.ctx, f.admin, exchange.GetId(), f.recipient, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))), faultErr)
	require.ErrorIs(t, faulty("set", 0x08).RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), faultErr)
	require.ErrorIs(t, faulty("set", 0x05).setNextExchangeID(f.ctx, 9), faultErr)
	require.ErrorIs(t, faulty("delete", 0x03).DeleteExchange(f.ctx, f.admin, deleteExchange.GetId()), faultErr)
	reserveBytes, err := f.accountCodec.StringToBytes(exchange.GetReserveAddress())
	require.NoError(t, err)
	_, err = faulty("get", 0x04).SendRestrictionFn(f.ctx, nil, sdk.AccAddress(reserveBytes), sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))
	require.ErrorIs(t, err, faultErr)
	for _, prefix := range []byte{0x01, 0x02, 0x03, 0x04, 0x06, 0x07, 0x08, 0x0a, 0x05} {
		require.ErrorIs(t, faulty("set", prefix).ImportGenesis(f.ctx, genesis), faultErr)
	}

	_, err = NewQueryServer(ptr(faulty("iterator", 0x02))).Exchanges(f.ctx, &bexv1.QueryExchangesRequest{})
	require.ErrorIs(t, err, faultErr)
	_, err = NewQueryServer(ptr(faulty("iterator", 0x03))).ExchangesByAdmin(f.ctx, &bexv1.QueryExchangesByAdminRequest{AdminAddress: f.admin})
	require.ErrorIs(t, err, faultErr)
	_, err = faulty("iterator", 0x01).ExportGenesis(f.ctx)
	require.ErrorIs(t, err, faultErr)
	for _, prefix := range []byte{0x02, 0x06, 0x07, 0x08, 0x0a} {
		_, err = faulty("iterator", prefix).ExportGenesis(f.ctx)
		require.ErrorIs(t, err, faultErr)
	}
	_, err = faulty("get", 0x05).ExportGenesis(f.ctx)
	require.ErrorIs(t, err, faultErr)
	require.ErrorIs(t, faulty("iterator", 0x06).AssertInvariants(f.ctx), faultErr)
	require.ErrorIs(t, faulty("get", 0x07).AssertInvariants(f.ctx), faultErr)
	require.ErrorIs(t, faulty("iterator", 0x06).AssertFeeSolvency(f.ctx), faultErr)
}

func bytesOf(b byte) []byte {
	out := make([]byte, 20)
	for i := range out {
		out[i] = b
	}
	return out
}

func ptr(k Keeper) *Keeper {
	return &k
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

type failingAddressCodec struct {
	base              address.Codec
	failBytesToString bool
	failBytesValue    []byte
	failStringValue   string
}

func (c failingAddressCodec) StringToBytes(text string) ([]byte, error) {
	if c.failStringValue != "" && text == c.failStringValue {
		return nil, errors.New("string-to-bytes fault")
	}
	return c.base.StringToBytes(text)
}

func (c failingAddressCodec) BytesToString(bz []byte) (string, error) {
	if c.failBytesToString || (len(c.failBytesValue) > 0 && string(c.failBytesValue) == string(bz)) {
		return "", errors.New("bytes-to-string fault")
	}
	return c.base.BytesToString(bz)
}
