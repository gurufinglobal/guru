package keeper

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type keeperTestFixture struct {
	ctx                sdk.Context
	keeper             Keeper
	storeService       corestore.KVStoreService
	accountCodec       address.Codec
	accountKeeper      *mockAccountKeeper
	bankKeeper         *mockBankKeeper
	oracleKeeper       *mockOracleKeeper
	constitutionKeeper *mockConstitutionKeeper
	channelKeeper      *mockChannelKeeper
	moderator          string
	admin              string
	adminAddr          sdk.AccAddress
	recipient          sdk.AccAddress
}

func setupKeeperFixture(t *testing.T) keeperTestFixture {
	return setupKeeperFixtureWithExtraKVStoreKeys(t, nil)
}

func setupKeeperFixtureWithExtraKVStoreKeys(t *testing.T, extraKeys map[string]*storetypes.KVStoreKey) keeperTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_bex_test")
	keys := map[string]*storetypes.KVStoreKey{types.StoreKey: key}
	for name, extraKey := range extraKeys {
		keys[name] = extraKey
	}
	ctx := testutil.DefaultContextWithKeys(
		keys,
		map[string]*storetypes.TransientStoreKey{"transient_bex_test": transientKey},
		nil,
	).WithBlockTime(time.Unix(1_700_000_000, 0))

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	moderator, moderatorAddr := testAddress(t, accountCodec, 0x01)
	admin, adminAddr := testAddress(t, accountCodec, 0x02)
	_, recipient := testAddress(t, accountCodec, 0x03)
	accountKeeper := newMockAccountKeeper()
	bankKeeper := newMockBankKeeper()
	oracleKeeper := newMockOracleKeeper()
	constitutionKeeper := newMockConstitutionKeeper(moderator)
	channelKeeper := newMockChannelKeeper()
	channelKeeper.SetChannel("transwap", "channel-0", channeltypes.OPEN)
	channelKeeper.SetChannel("transwap", "channel-1", channeltypes.OPEN)

	storeService := runtime.NewKVStoreService(key)
	keeper := NewKeeper(
		storeService,
		accountCodec,
		accountKeeper,
		bankKeeper,
		oracleKeeper,
		constitutionKeeper,
		channelKeeper,
	)
	keeper.RegisterSendRestriction()
	accountKeeper.SetAccount(ctx, accountKeeper.NewAccountWithAddress(ctx, moderatorAddr))
	accountKeeper.SetAccount(ctx, accountKeeper.NewAccountWithAddress(ctx, adminAddr))
	bankKeeper.SetBalance(adminAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1_000_000)))

	return keeperTestFixture{
		ctx:                ctx,
		keeper:             keeper,
		storeService:       storeService,
		accountCodec:       accountCodec,
		accountKeeper:      accountKeeper,
		bankKeeper:         bankKeeper,
		oracleKeeper:       oracleKeeper,
		constitutionKeeper: constitutionKeeper,
		channelKeeper:      channelKeeper,
		moderator:          moderator,
		admin:              admin,
		adminAddr:          adminAddr,
		recipient:          recipient,
	}
}

func TestNewKeeperPanicsOnDuplicateSchemaPrefix(t *testing.T) {
	old := types.VolumePruneCursorKey
	types.VolumePruneCursorKey = types.VolumeWindowKey
	defer func() { types.VolumePruneCursorKey = old }()

	key := storetypes.NewKVStoreKey(types.StoreKey)
	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	require.Panics(t, func() {
		NewKeeper(
			runtime.NewKVStoreService(key),
			accountCodec,
			newMockAccountKeeper(),
			newMockBankKeeper(),
			newMockOracleKeeper(),
			newMockConstitutionKeeper("moderator"),
			newMockChannelKeeper(),
		)
	})
}

func TestRegisterExchangeCreatesReserveAndBlocksDirectTransfers(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	expectedReserve, err := f.keeper.GetReserveAddressString(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, expectedReserve, exchange.GetReserveAddress())

	reserveBytes, err := f.accountCodec.StringToBytes(exchange.GetReserveAddress())
	require.NoError(t, err)
	reserveAddr := sdk.AccAddress(reserveBytes)
	require.NotNil(t, f.accountKeeper.GetAccount(f.ctx, reserveAddr))

	err = f.bankKeeper.SendCoins(f.ctx, f.adminAddr, reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))
	require.ErrorIs(t, err, types.ErrDirectReserveTransfer)

	wrongAllowanceCtx := f.keeper.WithReserveReceiveAllowance(f.ctx, exchange.GetId()+1)
	err = f.bankKeeper.SendCoins(wrongAllowanceCtx, f.adminAddr, reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 3)))
	require.ErrorIs(t, err, types.ErrDirectReserveTransfer)

	allowedCtx := f.keeper.WithReserveReceiveAllowance(f.ctx, exchange.GetId())
	require.NoError(t, f.bankKeeper.SendCoins(allowedCtx, f.adminAddr, reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 7))))

	unwrappedAllowedCtx := sdk.UnwrapSDKContext(f.keeper.WithReserveReceiveAllowance(f.ctx, exchange.GetId()))
	require.NoError(t, f.bankKeeper.SendCoins(unwrappedAllowedCtx, f.adminAddr, reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 5))))

	require.NoError(t, f.keeper.DepositReserve(f.ctx, f.admin, exchange.GetId(), sdk.NewCoins(sdk.NewInt64Coin("agxn", 10))))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 22)), f.bankKeeper.GetAllBalances(f.ctx, reserveAddr))
}

func TestUpdateExchangeRequiresRevisionAndBlocksActiveRouteChanges(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	_, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), 0, &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(30),
	})
	require.ErrorIs(t, err, types.ErrRevisionConflict)

	_, err = f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		DenomA: wrapperspb.String("agxn"),
	})
	require.ErrorIs(t, err, types.ErrInvalidRoute)

	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(30),
		LimitAToB:  wrapperspb.String("900"),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), updated.GetRevision())
	require.Equal(t, uint32(30), updated.GetFeeBpsAToB())
	require.Equal(t, "900", updated.GetLimitAToB())
}

func TestQuoteSwapUsesDirectionOracleCeilFeeAndVolumeCap(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Unix())

	quote, err := f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "101",
	})
	require.NoError(t, err)
	require.Equal(t, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, quote.GetDirection())
	require.Equal(t, exchange.GetIbcDenomB(), quote.GetOutputDenom())
	require.Equal(t, "1", quote.GetFeeAmount())
	require.Equal(t, "100", quote.GetNetAmountIn())
	require.Equal(t, "200", quote.GetAmountOut())

	f.channelKeeper.SetChannel("transwap", "channel-0", channeltypes.CLOSED)
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "101",
	})
	require.ErrorIs(t, err, types.ErrInvalidRoute)
	f.channelKeeper.SetChannel("transwap", "channel-0", channeltypes.OPEN)

	_, err = f.keeper.ResolveSwapDirection(f.ctx, exchange.GetId(), exchange.GetIbcDenomA())
	require.ErrorIs(t, err, types.ErrInvalidRoute)

	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(900)))
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "101",
	})
	require.ErrorIs(t, err, types.ErrVolumeCapExceeded)
}

func TestQuoteSwapOracleMutationAndRoundingBoundaries(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	exchange, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB:    wrapperspb.UInt32(25),
		LimitAToB:     wrapperspb.String("0"),
		VolumeCapAToB: wrapperspb.String("0"),
	})
	require.NoError(t, err)

	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10000",
	})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "0", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10000",
	})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "-1", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10000",
	})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "not-a-decimal", f.ctx.BlockTime().Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10000",
	})
	require.ErrorIs(t, err, types.ErrInvalidOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Add(-300*time.Second).Unix())
	quote, err := f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10000",
	})
	require.NoError(t, err)
	require.Equal(t, "25", quote.GetFeeAmount())
	require.Equal(t, "9975", quote.GetNetAmountIn())
	require.Equal(t, "19950", quote.GetAmountOut())

	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Add(-301*time.Second).Unix())
	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10000",
	})
	require.ErrorIs(t, err, types.ErrStaleOracleRate)

	f.oracleKeeper.SetValue("AGXN/GXUSD", "3", f.ctx.BlockTime().Unix())
	quote, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10000",
	})
	require.NoError(t, err)
	require.Equal(t, "29925", quote.GetAmountOut())

	require.Equal(t, sdkmath.NewInt(1), ceilFee(sdkmath.NewInt(1), 1))
	require.Equal(t, sdkmath.NewInt(25), ceilFee(sdkmath.NewInt(10000), 25))
	require.Equal(t, sdkmath.NewInt(26), ceilFee(sdkmath.NewInt(10001), 25))
	require.Equal(t, sdkmath.NewInt(308642), ceilFee(sdkmath.NewInt(123456789), 25))

	exchange, err = f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB: wrapperspb.UInt32(0),
	})
	require.NoError(t, err)
	f.oracleKeeper.SetValue("AGXN/GXUSD", "1.5", f.ctx.BlockTime().Unix())
	quote, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "3",
	})
	require.NoError(t, err)
	require.Equal(t, "4", quote.GetAmountOut())
}

func TestQuoteSwapLimitAndVolumeCapBoundaries(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	f.oracleKeeper.SetValue("AGXN/GXUSD", "1", f.ctx.BlockTime().Unix())

	exchange, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB:    wrapperspb.UInt32(0),
		LimitAToB:     wrapperspb.String("100"),
		VolumeCapAToB: wrapperspb.String("0"),
	})
	require.NoError(t, err)

	quote, err := f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "99",
	})
	require.NoError(t, err)
	require.Equal(t, "99", quote.GetAmountOut())

	quote, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "100",
	})
	require.NoError(t, err)
	require.Equal(t, "100", quote.GetAmountOut())

	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "101",
	})
	require.ErrorIs(t, err, types.ErrOutputLimitExceeded)

	exchange, err = f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		LimitAToB:     wrapperspb.String("0"),
		VolumeCapAToB: wrapperspb.String("150"),
	})
	require.NoError(t, err)

	quote, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "150",
	})
	require.NoError(t, err)
	require.Equal(t, "150", quote.GetAmountOut())

	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "151",
	})
	require.ErrorIs(t, err, types.ErrVolumeCapExceeded)

	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(50)))
	quote, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "100",
	})
	require.NoError(t, err)
	require.Equal(t, "50", quote.GetVolumeUsed())
	require.Equal(t, "150", quote.GetVolumeCap())

	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "101",
	})
	require.ErrorIs(t, err, types.ErrVolumeCapExceeded)

	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(100)))
	used, err := f.keeper.GetCurrentVolumeAmount(f.ctx, exchange, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B)
	require.NoError(t, err)
	require.Equal(t, "150", used.String())
	require.ErrorIs(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, sdkmath.NewInt(1)), types.ErrVolumeCapExceeded)
}

func TestQuoteAndRecordUsePendingEpochWindowConsistently(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	direction := bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B
	f.oracleKeeper.SetValue("AGXN/GXUSD", "1", f.ctx.BlockTime().Unix())

	exchange, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		FeeBpsAToB:    wrapperspb.UInt32(0),
		LimitAToB:     wrapperspb.String("0"),
		VolumeCapAToB: wrapperspb.String("100"),
	})
	require.NoError(t, err)

	require.NoError(t, f.keeper.RecordVolumeWindow(f.ctx, exchange.GetId(), direction, sdkmath.NewInt(90)))
	oldKey := currentVolumeKey(f.ctx.BlockTime(), exchange.GetId(), direction, minVolumeEpochSecs)
	oldValue, err := f.keeper.volumeWindow.Get(f.ctx, oldKey)
	require.NoError(t, err)
	require.Equal(t, "90", oldValue)

	effectiveAt := uint64(f.ctx.BlockTime().Add(time.Second).Unix())
	exchange, err = f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &bexv1.ExchangeUpdatePatch{
		PendingVolumeEpochSeconds:         wrapperspb.UInt32(minVolumeEpochSecs * 2),
		PendingVolumeEpochEffectiveAtUnix: wrapperspb.UInt64(effectiveAt),
	})
	require.NoError(t, err)

	quote, err := f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "10",
	})
	require.NoError(t, err)
	require.Equal(t, "90", quote.GetVolumeUsed())
	require.Equal(t, "10", quote.GetAmountOut())

	_, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "11",
	})
	require.ErrorIs(t, err, types.ErrVolumeCapExceeded)

	futureCtx := f.ctx.WithBlockTime(time.Unix(int64(effectiveAt), 0))
	quote, err = f.keeper.QuoteSwap(futureCtx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "100",
	})
	require.NoError(t, err)
	require.Equal(t, "0", quote.GetVolumeUsed())
	require.Equal(t, "100", quote.GetAmountOut())

	queryServer := NewQueryServer(&f.keeper)
	window, err := queryServer.VolumeWindow(futureCtx, &bexv1.QueryVolumeWindowRequest{
		ExchangeId: exchange.GetId(),
		Direction:  direction,
	})
	require.NoError(t, err)
	require.Equal(t, minVolumeEpochSecs*2, window.GetWindow().GetEpochSeconds())
	require.Equal(t, "0", window.GetWindow().GetAmount())

	persisted, err := f.keeper.GetExchange(futureCtx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, minVolumeEpochSecs, persisted.GetVolumeEpochSeconds())
	require.Equal(t, minVolumeEpochSecs*2, persisted.GetPendingVolumeEpochSeconds())
	require.Equal(t, effectiveAt, persisted.GetPendingVolumeEpochEffectiveAtUnix())
	require.Equal(t, exchange.GetRevision(), persisted.GetRevision())

	require.NoError(t, f.keeper.RecordVolumeWindow(futureCtx, exchange.GetId(), direction, sdkmath.NewInt(100)))
	persisted, err = f.keeper.GetExchange(futureCtx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, minVolumeEpochSecs*2, persisted.GetVolumeEpochSeconds())
	require.Zero(t, persisted.GetPendingVolumeEpochSeconds())
	require.Zero(t, persisted.GetPendingVolumeEpochEffectiveAtUnix())
	require.Equal(t, exchange.GetRevision()+1, persisted.GetRevision())

	newKey := currentVolumeKey(futureCtx.BlockTime(), exchange.GetId(), direction, minVolumeEpochSecs*2)
	newValue, err := f.keeper.volumeWindow.Get(futureCtx, newKey)
	require.NoError(t, err)
	require.Equal(t, "100", newValue)
	oldValue, err = f.keeper.volumeWindow.Get(futureCtx, oldKey)
	require.NoError(t, err)
	require.Equal(t, "90", oldValue)
	require.ErrorIs(t, f.keeper.RecordVolumeWindow(futureCtx, exchange.GetId(), direction, sdkmath.NewInt(1)), types.ErrVolumeCapExceeded)
}

func TestExchangeReadinessValidatesOracleValue(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	queryServer := NewQueryServer(&f.keeper)
	req := &bexv1.QueryExchangeReadinessRequest{
		ExchangeId: exchange.GetId(),
		Direction:  bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
	}

	resp, err := queryServer.ExchangeReadiness(f.ctx, req)
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Contains(t, resp.GetReadiness().GetBlockingReasons(), "oracle value unavailable")

	f.oracleKeeper.SetValue("AGXN/GXUSD", "0", f.ctx.BlockTime().Unix())
	resp, err = queryServer.ExchangeReadiness(f.ctx, req)
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Contains(t, resp.GetReadiness().GetBlockingReasons(), "oracle value is not a positive decimal")

	f.oracleKeeper.SetValue("AGXN/GXUSD", "-1", f.ctx.BlockTime().Unix())
	resp, err = queryServer.ExchangeReadiness(f.ctx, req)
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Contains(t, resp.GetReadiness().GetBlockingReasons(), "oracle value is not a positive decimal")

	f.oracleKeeper.SetValue("AGXN/GXUSD", "not-a-decimal", f.ctx.BlockTime().Unix())
	resp, err = queryServer.ExchangeReadiness(f.ctx, req)
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Contains(t, resp.GetReadiness().GetBlockingReasons(), "oracle value is not a positive decimal")

	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Add(-time.Hour).Unix())
	resp, err = queryServer.ExchangeReadiness(f.ctx, req)
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Contains(t, resp.GetReadiness().GetBlockingReasons(), "oracle value is stale")

	f.oracleKeeper.SetValue("AGXN/GXUSD", "2", f.ctx.BlockTime().Unix())
	resp, err = queryServer.ExchangeReadiness(f.ctx, req)
	require.NoError(t, err)
	require.True(t, resp.GetReadiness().GetReady())
	require.Empty(t, resp.GetReadiness().GetBlockingReasons())

	f.channelKeeper.SetChannel("transwap", "channel-0", channeltypes.CLOSED)
	resp, err = queryServer.ExchangeReadiness(f.ctx, req)
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Len(t, resp.GetReadiness().GetBlockingReasons(), 1)
	require.Contains(t, resp.GetReadiness().GetBlockingReasons()[0], "not open")
}

func TestExchangeReadinessReportsInactiveDeletedMissingAndInvalidDirection(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	q := NewQueryServer(&f.keeper)

	inactive := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	resp, err := q.ExchangeReadiness(f.ctx, &bexv1.QueryExchangeReadinessRequest{
		ExchangeId: inactive.GetId(),
		Direction:  bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
	})
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Contains(t, resp.GetReadiness().GetBlockingReasons(), "exchange is not active")

	deleted := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, deleted.GetId()))
	resp, err = q.ExchangeReadiness(f.ctx, &bexv1.QueryExchangeReadinessRequest{
		ExchangeId: deleted.GetId(),
		Direction:  bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
	})
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Len(t, resp.GetReadiness().GetBlockingReasons(), 1)
	require.Contains(t, resp.GetReadiness().GetBlockingReasons()[0], "is deleted")

	resp, err = q.ExchangeReadiness(f.ctx, &bexv1.QueryExchangeReadinessRequest{
		ExchangeId: 999,
		Direction:  bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
	})
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Len(t, resp.GetReadiness().GetBlockingReasons(), 1)
	require.Contains(t, resp.GetReadiness().GetBlockingReasons()[0], "not found")

	active := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	resp, err = q.ExchangeReadiness(f.ctx, &bexv1.QueryExchangeReadinessRequest{
		ExchangeId: active.GetId(),
		Direction:  bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED,
	})
	require.NoError(t, err)
	require.False(t, resp.GetReadiness().GetReady())
	require.Contains(t, resp.GetReadiness().GetBlockingReasons(), "invalid direction: invalid route")
}

func TestDeleteExchangeRequiresInactiveZeroReserveAndZeroFees(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	reserveBytes, err := f.accountCodec.StringToBytes(exchange.GetReserveAddress())
	require.NoError(t, err)
	reserveAddr := sdk.AccAddress(reserveBytes)

	f.bankKeeper.SetBalance(reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))
	require.ErrorIs(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()), types.ErrInsufficientReserve)

	f.bankKeeper.SetBalance(reserveAddr, sdk.Coins{})
	require.NoError(t, f.keeper.AddCollectedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)))
	require.ErrorIs(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()), types.ErrInsufficientAvailableFees)

	require.NoError(t, f.keeper.DeductCollectedFee(f.ctx, exchange.GetId(), sdk.NewInt64Coin("agxn", 1)))
	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()))

	deleted, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED, deleted.GetStatus())
	owner, err := f.keeper.reserveByAddress.Get(f.ctx, exchange.GetReserveAddress())
	require.NoError(t, err)
	require.Equal(t, exchange.GetId(), owner)

	err = f.bankKeeper.SendCoins(f.ctx, f.adminAddr, reserveAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))
	require.ErrorIs(t, err, types.ErrDirectReserveTransfer)
	require.True(t, f.bankKeeper.GetAllBalances(f.ctx, reserveAddr).IsZero())
}

func registerExchange(t *testing.T, f keeperTestFixture, status bexv1.ExchangeStatus) *bexv1.Exchange {
	t.Helper()

	exchange, err := f.keeper.RegisterExchange(f.ctx, validRegisterExchangeMsg(f.admin, status))
	require.NoError(t, err)
	return exchange
}

func validRegisterExchangeMsg(admin string, status bexv1.ExchangeStatus) *bexv1.MsgRegisterExchange {
	return &bexv1.MsgRegisterExchange{
		AdminAddress:              admin,
		DenomA:                    "agxn",
		PortA:                     "transwap",
		ChannelA:                  "channel-0",
		DenomB:                    "gxusd",
		PortB:                     "transwap",
		ChannelB:                  "channel-1",
		OracleSymbolAToB:          "AGXN/GXUSD",
		OracleSymbolBToA:          "GXUSD/AGXN",
		FeeBpsAToB:                25,
		FeeBpsBToA:                10,
		LimitAToB:                 "10000",
		LimitBToA:                 "10000",
		VolumeCapAToB:             "1000",
		VolumeCapBToA:             "1000",
		Status:                    status,
		Metadata:                  map[string]string{"venue": "bex-test"},
		VolumeEpochSeconds:        minVolumeEpochSecs,
		MaxOracleStalenessSeconds: 300,
	}
}

func testAddress(t *testing.T, accountCodec address.Codec, b byte) (string, sdk.AccAddress) {
	t.Helper()

	addr := sdk.AccAddress(bytes.Repeat([]byte{b}, 20))
	encoded, err := accountCodec.BytesToString(addr)
	require.NoError(t, err)
	return encoded, addr
}

func requireEventTypes(t *testing.T, ctx sdk.Context, expected ...string) {
	t.Helper()

	seen := map[string]struct{}{}
	for _, event := range ctx.EventManager().Events() {
		seen[event.Type] = struct{}{}
	}
	for _, eventType := range expected {
		require.Contains(t, seen, eventType)
	}
}

type mockAccountKeeper struct {
	accounts map[string]sdk.AccountI
}

func newMockAccountKeeper() *mockAccountKeeper {
	return &mockAccountKeeper{accounts: map[string]sdk.AccountI{}}
}

func (m *mockAccountKeeper) NewAccountWithAddress(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(addr)
}

func (m *mockAccountKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return m.accounts[string(addr)]
}

func (m *mockAccountKeeper) SetAccount(_ context.Context, account sdk.AccountI) {
	m.accounts[string(account.GetAddress())] = account
}

func (m *mockAccountKeeper) AddressCodec() address.Codec {
	return evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
}

type mockBankKeeper struct {
	balances     map[string]sdk.Coins
	restrictions []banktypes.SendRestrictionFn
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{balances: map[string]sdk.Coins{}}
}

func (m *mockBankKeeper) AppendSendRestriction(restriction banktypes.SendRestrictionFn) {
	m.restrictions = append(m.restrictions, restriction)
}

func (m *mockBankKeeper) SetBalance(addr sdk.AccAddress, coins sdk.Coins) {
	m.balances[string(addr)] = coins.Sort()
}

func (m *mockBankKeeper) GetAllBalances(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[string(addr)]
}

func (m *mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.GetAllBalances(ctx, addr)
}

func (m *mockBankKeeper) IsSendEnabledCoins(context.Context, ...sdk.Coin) error {
	return nil
}

func (m *mockBankKeeper) BlockedAddr(sdk.AccAddress) bool {
	return false
}

func (m *mockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.GetAllBalances(ctx, addr).AmountOf(denom))
}

func (m *mockBankKeeper) SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error {
	nextTo := toAddr
	for _, restriction := range m.restrictions {
		rewritten, err := restriction(ctx, fromAddr, nextTo, amt)
		if err != nil {
			return err
		}
		nextTo = rewritten
	}
	if !hasCoins(m.GetAllBalances(ctx, fromAddr), amt) {
		return fmt.Errorf("insufficient account funds")
	}
	m.balances[string(fromAddr)] = m.GetAllBalances(ctx, fromAddr).Sub(amt...)
	m.balances[string(nextTo)] = m.GetAllBalances(ctx, nextTo).Add(amt...)
	return nil
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return m.SendCoins(ctx, senderAddr, authtypes.NewModuleAddress(recipientModule), amt)
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	moduleAddr := authtypes.NewModuleAddress(senderModule)
	if !hasCoins(m.GetAllBalances(ctx, moduleAddr), amt) {
		return fmt.Errorf("insufficient module funds")
	}
	m.balances[string(moduleAddr)] = m.GetAllBalances(ctx, moduleAddr).Sub(amt...)
	m.balances[string(recipientAddr)] = m.GetAllBalances(ctx, recipientAddr).Add(amt...)
	return nil
}

type mockOracleKeeper struct {
	values map[string]*oraclev1.OracleValue
}

func newMockOracleKeeper() *mockOracleKeeper {
	return &mockOracleKeeper{values: map[string]*oraclev1.OracleValue{}}
}

func (m *mockOracleKeeper) SetValue(symbol, value string, blockTimeUnix int64) {
	m.values[symbol] = &oraclev1.OracleValue{
		Symbol:        symbol,
		ValueType:     oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:         value,
		BlockHeight:   1,
		BlockTimeUnix: blockTimeUnix,
	}
}

func (m *mockOracleKeeper) GetLatestValue(_ context.Context, symbol string) (*oraclev1.OracleValue, error) {
	value, ok := m.values[symbol]
	if !ok {
		return nil, fmt.Errorf("oracle value %s not found", symbol)
	}
	return value, nil
}

type mockConstitutionKeeper struct {
	moderatorAddress string
	err              error
}

func newMockConstitutionKeeper(moderatorAddress string) *mockConstitutionKeeper {
	return &mockConstitutionKeeper{moderatorAddress: moderatorAddress}
}

func (m *mockConstitutionKeeper) GetModeratorAddress(context.Context) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.moderatorAddress == "" {
		return "", collections.ErrNotFound
	}
	return m.moderatorAddress, nil
}

type mockChannelKeeper struct {
	channels map[string]channeltypes.Channel
}

func newMockChannelKeeper() *mockChannelKeeper {
	return &mockChannelKeeper{channels: map[string]channeltypes.Channel{}}
}

func (m *mockChannelKeeper) SetChannel(portID, channelID string, state channeltypes.State) {
	m.channels[portID+"/"+channelID] = channeltypes.Channel{State: state}
}

func (m *mockChannelKeeper) GetChannel(_ sdk.Context, portID, channelID string) (channeltypes.Channel, bool) {
	channel, ok := m.channels[portID+"/"+channelID]
	return channel, ok
}
