package keeper

import (
	"context"
	"sort"
	"testing"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

type reserveAllowanceOuterContextKey struct{}

func TestReserveDepositorAdminLifecycleCanonicalStorage(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)

	depositor, depositorAddr := testAddress(t, f.accountCodec, 0x11)
	depositorAlias := common.BytesToAddress(depositorAddr).Hex()
	otherAdmin, _ := testAddress(t, f.accountCodec, 0x12)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, otherAdmin))

	err := f.keeper.AddReserveDepositor(f.ctx, depositor, exchange.GetId(), depositorAlias)
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	err = f.keeper.AddReserveDepositor(f.ctx, otherAdmin, exchange.GetId(), depositorAlias)
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)

	require.NoError(t, f.keeper.AddReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositorAlias))
	hasCanonical, err := f.keeper.reserveDepositors.Has(f.ctx, collections.Join(exchange.GetId(), depositor))
	require.NoError(t, err)
	require.True(t, hasCanonical)
	hasAlias, err := f.keeper.reserveDepositors.Has(f.ctx, collections.Join(exchange.GetId(), depositorAlias))
	require.NoError(t, err)
	require.False(t, hasAlias)

	isDepositor, err := f.keeper.IsReserveDepositor(f.ctx, exchange.GetId(), depositorAlias)
	require.NoError(t, err)
	require.True(t, isDepositor)
	require.ErrorIs(
		t,
		f.keeper.AddReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositor),
		types.ErrInvalidRequest,
	)

	afterAdd, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, exchange.GetRevision(), afterAdd.GetRevision(), "allowlist changes must not revise exchange config")

	err = f.keeper.RemoveReserveDepositor(f.ctx, otherAdmin, exchange.GetId(), depositor)
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	require.NoError(t, f.keeper.RemoveReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositorAlias))
	isDepositor, err = f.keeper.IsReserveDepositor(f.ctx, exchange.GetId(), depositor)
	require.NoError(t, err)
	require.False(t, isDepositor)
	require.ErrorIs(
		t,
		f.keeper.RemoveReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositor),
		types.ErrUnauthorizedReserveDepositor,
	)

	afterRemove, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, exchange.GetRevision(), afterRemove.GetRevision(), "allowlist changes must not revise exchange config")
	requireEventTypes(t, f.ctx, types.EventTypeReserveDepositorAdded, types.EventTypeReserveDepositorRemoved)
}

func TestReserveDepositorDepositsActiveAndInactiveWithoutDirectSendBypass(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	active := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	inactive := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	depositor, depositorAddr := testAddress(t, f.accountCodec, 0x21)
	unauthorized, unauthorizedAddr := testAddress(t, f.accountCodec, 0x22)
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, depositorAddr))
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, unauthorizedAddr))
	f.bankKeeper.SetBalance(depositorAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1_000)))
	f.bankKeeper.SetBalance(unauthorizedAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 100)))

	require.NoError(t, f.keeper.AddReserveDepositor(f.ctx, f.admin, active.GetId(), depositor))
	require.NoError(t, f.keeper.AddReserveDepositor(f.ctx, f.admin, inactive.GetId(), depositor))
	isAdmin, err := f.keeper.IsAdmin(f.ctx, depositor)
	require.NoError(t, err)
	require.False(t, isAdmin, "reserve deposit privilege must not grant BEX admin privilege")

	activeReserveBytes, err := f.accountCodec.StringToBytes(active.GetReserveAddress())
	require.NoError(t, err)
	activeReserve := sdk.AccAddress(activeReserveBytes)
	depositorBefore := f.bankKeeper.GetAllBalances(f.ctx, depositorAddr)
	err = f.bankKeeper.SendCoins(f.ctx, depositorAddr, activeReserve, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)))
	require.ErrorIs(t, err, types.ErrDirectReserveTransfer)
	require.Equal(t, depositorBefore, f.bankKeeper.GetAllBalances(f.ctx, depositorAddr))
	require.Empty(t, f.bankKeeper.GetAllBalances(f.ctx, activeReserve))

	require.NoError(t, f.keeper.DepositReserve(
		f.ctx,
		depositor,
		active.GetId(),
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 11)),
	))
	require.NoError(t, f.keeper.DepositReserve(
		f.ctx,
		depositor,
		inactive.GetId(),
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 13)),
	))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 11)), f.bankKeeper.GetAllBalances(f.ctx, activeReserve))

	inactiveReserveBytes, err := f.accountCodec.StringToBytes(inactive.GetReserveAddress())
	require.NoError(t, err)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 13)),
		f.bankKeeper.GetAllBalances(f.ctx, sdk.AccAddress(inactiveReserveBytes)),
	)

	err = f.keeper.DepositReserve(
		f.ctx,
		unauthorized,
		active.GetId(),
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
	)
	require.ErrorIs(t, err, types.ErrUnauthorizedReserveDepositor)

	require.NoError(t, f.keeper.RemoveReserveDepositor(f.ctx, f.admin, active.GetId(), depositor))
	err = f.keeper.DepositReserve(
		f.ctx,
		depositor,
		active.GetId(),
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
	)
	require.ErrorIs(t, err, types.ErrUnauthorizedReserveDepositor)
	requireEventTypes(t, f.ctx, types.EventTypeReserveDeposited)
}

func TestPreviousExchangeAdminCanDepositOnlyThroughExplicitAllowlist(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	require.NoError(t, f.keeper.AddReserveDepositor(f.ctx, f.admin, exchange.GetId(), f.admin))
	newAdmin, _ := testAddress(t, f.accountCodec, 0x04)
	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &types.ExchangeUpdatePatch{
		NewAdminAddress: types.NewStringValue(newAdmin),
	})
	require.NoError(t, err)
	require.Equal(t, newAdmin, updated.GetAdminAddress())
	isBEXAdmin, err := f.keeper.IsAdmin(f.ctx, newAdmin)
	require.NoError(t, err)
	require.False(t, isBEXAdmin, "exchange ownership must not require BEX admin registration")

	require.NoError(t, f.keeper.DepositReserve(
		f.ctx,
		f.admin,
		exchange.GetId(),
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
	))

	require.NoError(t, f.keeper.RemoveReserveDepositor(f.ctx, newAdmin, exchange.GetId(), f.admin))
	require.ErrorIs(
		t,
		f.keeper.DepositReserve(
			f.ctx,
			f.admin,
			exchange.GetId(),
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		),
		types.ErrUnauthorizedReserveDepositor,
	)
}

func TestReserveDepositorHasNoAdministrativeOrWithdrawalPrivilegesAndDeletedIsTerminal(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	depositor, depositorAddr := testAddress(t, f.accountCodec, 0x31)
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, depositorAddr))
	f.bankKeeper.SetBalance(depositorAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))
	require.NoError(t, f.keeper.AddReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositor))

	amount := sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))
	err := f.keeper.WithdrawReserve(f.ctx, depositor, exchange.GetId(), f.recipient, amount)
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	_, err = f.keeper.UpdateExchange(f.ctx, depositor, exchange.GetId(), exchange.GetRevision(), &types.ExchangeUpdatePatch{
		FeeBpsAToB: types.NewUInt32Value(exchange.GetFeeBpsAToB() + 1),
	})
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	err = f.keeper.DeleteExchange(f.ctx, depositor, exchange.GetId())
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)
	err = f.keeper.WithdrawFees(f.ctx, depositor, exchange.GetId(), f.recipient, amount)
	require.ErrorIs(t, err, types.ErrWrongExchangeAdmin)

	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()))
	deleted, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.Equal(t, types.ExchangeStatus_EXCHANGE_STATUS_DELETED, deleted.GetStatus())
	require.Equal(t, exchange.GetRevision()+1, deleted.GetRevision())

	isDepositor, err := f.keeper.IsReserveDepositor(f.ctx, exchange.GetId(), depositor)
	require.NoError(t, err)
	require.True(t, isDepositor, "deleted exchanges retain their allowlist for historical state")
	require.ErrorIs(
		t,
		f.keeper.AddReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositor),
		types.ErrExchangeDeleted,
	)
	require.ErrorIs(
		t,
		f.keeper.RemoveReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositor),
		types.ErrExchangeDeleted,
	)
	require.ErrorIs(
		t,
		f.keeper.DepositReserve(f.ctx, depositor, exchange.GetId(), amount),
		types.ErrExchangeDeleted,
	)

	reserveBytes, err := f.accountCodec.StringToBytes(exchange.GetReserveAddress())
	require.NoError(t, err)
	err = f.bankKeeper.SendCoins(f.ctx, depositorAddr, sdk.AccAddress(reserveBytes), amount)
	require.ErrorIs(t, err, types.ErrDirectReserveTransfer, "allowlist and deletion must never bypass bank restriction")
}

func TestReserveDepositorQueriesMembershipAndSDKPagination(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	pageRequest := &sdkquery.PageRequest{
		Key:        []byte("key"),
		Offset:     1001,
		Limit:      1001,
		CountTotal: true,
		Reverse:    true,
	}
	require.Equal(t, []byte("key"), pageRequest.GetKey())
	require.Equal(t, uint64(1001), pageRequest.GetOffset())
	require.Equal(t, uint64(1001), pageRequest.GetLimit())
	require.True(t, pageRequest.GetCountTotal())
	require.True(t, pageRequest.GetReverse())

	want := make([]string, 0, 3)
	for _, b := range []byte{0x43, 0x41, 0x42} {
		depositor, _ := testAddress(t, f.accountCodec, b)
		want = append(want, depositor)
		require.NoError(t, f.keeper.AddReserveDepositor(f.ctx, f.admin, exchange.GetId(), depositor))
	}
	sort.Strings(want)

	queryServer := NewQueryServer(&f.keeper)
	counted, err := queryServer.ReserveDepositors(f.ctx, &types.QueryReserveDepositorsRequest{
		ExchangeId: exchange.GetId(),
		Pagination: &sdkquery.PageRequest{Offset: 1, Limit: 1, CountTotal: true},
	})
	require.NoError(t, err)
	require.Equal(t, want[1:2], counted.GetDepositors())
	require.Equal(t, uint64(len(want)), counted.GetPagination().GetTotal())

	first, err := queryServer.ReserveDepositors(f.ctx, &types.QueryReserveDepositorsRequest{
		ExchangeId: exchange.GetId(),
		Pagination: &sdkquery.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Equal(t, want[:2], first.GetDepositors())
	require.NotEmpty(t, first.GetPagination().GetNextKey())
	require.Zero(t, first.GetPagination().GetTotal())
	_, err = queryServer.ReserveDepositors(f.ctx, &types.QueryReserveDepositorsRequest{
		ExchangeId: exchange.GetId(),
		Pagination: &sdkquery.PageRequest{
			Key:    first.GetPagination().GetNextKey(),
			Offset: 1,
		},
	})
	require.ErrorContains(t, err, "either offset or key")

	second, err := queryServer.ReserveDepositors(f.ctx, &types.QueryReserveDepositorsRequest{
		ExchangeId: exchange.GetId(),
		Pagination: &sdkquery.PageRequest{
			Key:   first.GetPagination().GetNextKey(),
			Limit: 2,
		},
	})
	require.NoError(t, err)
	require.Equal(t, want[2:], second.GetDepositors())
	require.Empty(t, second.GetPagination().GetNextKey())

	membership, err := queryServer.IsReserveDepositor(f.ctx, &types.QueryIsReserveDepositorRequest{
		ExchangeId:       exchange.GetId(),
		DepositorAddress: want[1],
	})
	require.NoError(t, err)
	require.True(t, membership.GetIsReserveDepositor())
	nonMember, _ := testAddress(t, f.accountCodec, 0x44)
	membership, err = queryServer.IsReserveDepositor(f.ctx, &types.QueryIsReserveDepositorRequest{
		ExchangeId:       exchange.GetId(),
		DepositorAddress: nonMember,
	})
	require.NoError(t, err)
	require.False(t, membership.GetIsReserveDepositor())

	require.NoError(t, f.keeper.DeleteExchange(f.ctx, f.admin, exchange.GetId()))
	afterDelete, err := queryServer.ReserveDepositors(f.ctx, &types.QueryReserveDepositorsRequest{ExchangeId: exchange.GetId()})
	require.NoError(t, err)
	require.Equal(t, want, afterDelete.GetDepositors())
	require.Equal(t, uint64(len(want)), afterDelete.GetPagination().GetTotal())
	membership, err = queryServer.IsReserveDepositor(f.ctx, &types.QueryIsReserveDepositorRequest{
		ExchangeId:       exchange.GetId(),
		DepositorAddress: want[0],
	})
	require.NoError(t, err)
	require.True(t, membership.GetIsReserveDepositor())

	_, err = queryServer.ReserveDepositors(f.ctx, nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = queryServer.IsReserveDepositor(f.ctx, nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = queryServer.ReserveDepositors(f.ctx, &types.QueryReserveDepositorsRequest{ExchangeId: exchange.GetId() + 100})
	require.ErrorIs(t, err, types.ErrExchangeNotFound)
}

func TestReserveReceiveAllowancePreservesSDKContext(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	reserveBytes, err := f.accountCodec.StringToBytes(exchange.GetReserveAddress())
	require.NoError(t, err)
	reserveAddr := sdk.AccAddress(reserveBytes)

	allowed := f.keeper.withReserveReceiveAllowance(f.ctx, exchange.GetId())
	allowedSDK, ok := allowed.(sdk.Context)
	require.True(t, ok, "allowance must retain sdk.Context as the concrete context type")
	require.Equal(t, f.ctx.BlockTime(), allowedSDK.BlockTime())
	unwrapped := sdk.UnwrapSDKContext(allowed)
	require.Equal(t, f.ctx.BlockTime(), unwrapped.BlockTime())
	allowedExchangeID, ok := reserveAllowance(unwrapped)
	require.True(t, ok)
	require.Equal(t, exchange.GetId(), allowedExchangeID)

	require.NoError(t, f.bankKeeper.SendCoins(
		allowed,
		f.adminAddr,
		reserveAddr,
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 3)),
	))

	outerKey := reserveAllowanceOuterContextKey{}
	wrapped := context.WithValue(f.ctx, outerKey, "preserved")
	wrappedAllowed := f.keeper.withReserveReceiveAllowance(wrapped, exchange.GetId())
	wrappedSDK := sdk.UnwrapSDKContext(wrappedAllowed)
	require.Equal(t, "preserved", wrappedSDK.Value(outerKey))
	allowedExchangeID, ok = reserveAllowance(wrappedSDK)
	require.True(t, ok)
	require.Equal(t, exchange.GetId(), allowedExchangeID)
	require.NoError(t, f.bankKeeper.SendCoins(
		wrappedAllowed,
		f.adminAddr,
		reserveAddr,
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
	))

	wrongAllowance := f.keeper.withReserveReceiveAllowance(f.ctx, exchange.GetId()+1)
	err = f.bankKeeper.SendCoins(
		wrongAllowance,
		f.adminAddr,
		reserveAddr,
		sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
	)
	require.ErrorIs(t, err, types.ErrDirectReserveTransfer)
}

func TestReserveAccountIsKeylessBaseAccountAndRejectsUnsafeExistingAccounts(t *testing.T) {
	t.Run("creates keyless base account", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
		reserve := f.keeper.GetReserveAddress(f.ctx, exchange.GetId())
		account := f.accountKeeper.GetAccount(f.ctx, reserve)
		baseAccount, ok := account.(*authtypes.BaseAccount)
		require.True(t, ok)
		require.NotNil(t, baseAccount)
		require.True(t, baseAccount.GetAddress().Equals(reserve))
		require.Nil(t, baseAccount.GetPubKey())
		_, isModuleAccount := account.(sdk.ModuleAccountI)
		require.False(t, isModuleAccount)
	})

	t.Run("accepts an existing keyless base account", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		reserve := f.keeper.GetReserveAddress(f.ctx, DefaultNextExchangeID)
		existing := authtypes.NewBaseAccount(reserve, nil, 77, 9)
		f.accountKeeper.SetAccount(f.ctx, existing)

		exchange, err := f.keeper.RegisterExchange(
			f.ctx,
			validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE),
		)
		require.NoError(t, err)
		require.Equal(t, DefaultNextExchangeID, exchange.GetId())
		stored, ok := f.accountKeeper.GetAccount(f.ctx, reserve).(*authtypes.BaseAccount)
		require.True(t, ok)
		require.Equal(t, uint64(77), stored.GetAccountNumber())
		require.Equal(t, uint64(9), stored.GetSequence())
	})

	t.Run("reclaims a pre-created undelegated keyless vesting account", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		reserve := f.keeper.GetReserveAddress(f.ctx, DefaultNextExchangeID)
		base := authtypes.NewBaseAccount(reserve, nil, 77, 9)
		vestingBase, err := vestingtypes.NewBaseVestingAccount(
			base,
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 5)),
			f.ctx.BlockTime().AddDate(1, 0, 0).Unix(),
		)
		require.NoError(t, err)
		f.accountKeeper.SetAccount(f.ctx, vestingtypes.NewDelayedVestingAccountRaw(vestingBase))
		f.bankKeeper.SetBalance(reserve, sdk.NewCoins(sdk.NewInt64Coin("agxn", 5)))

		exchange, err := f.keeper.RegisterExchange(
			f.ctx,
			validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE),
		)
		require.NoError(t, err)
		require.Equal(t, DefaultNextExchangeID, exchange.GetId())
		recovered, ok := f.accountKeeper.GetAccount(f.ctx, reserve).(*authtypes.BaseAccount)
		require.True(t, ok)
		require.Nil(t, recovered.GetPubKey())
		require.Equal(t, uint64(77), recovered.GetAccountNumber())
		require.Equal(t, uint64(9), recovered.GetSequence())
		require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 5)), f.bankKeeper.GetAllBalances(f.ctx, reserve))
	})

	t.Run("rejects a pre-created vesting account with delegated funds", func(t *testing.T) {
		f := setupKeeperFixture(t)
		require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
		reserve := f.keeper.GetReserveAddress(f.ctx, DefaultNextExchangeID)
		base := authtypes.NewBaseAccount(reserve, nil, 0, 0)
		vestingBase, err := vestingtypes.NewBaseVestingAccount(
			base,
			sdk.NewCoins(sdk.NewInt64Coin("agxn", 5)),
			f.ctx.BlockTime().AddDate(1, 0, 0).Unix(),
		)
		require.NoError(t, err)
		vestingBase.DelegatedVesting = sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))
		f.accountKeeper.SetAccount(f.ctx, vestingtypes.NewDelayedVestingAccountRaw(vestingBase))

		_, err = f.keeper.RegisterExchange(
			f.ctx,
			validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE),
		)
		require.ErrorIs(t, err, types.ErrInvariantViolation)
	})

	for _, tc := range []struct {
		name    string
		account func(sdk.AccAddress) sdk.AccountI
	}{
		{
			name: "public key",
			account: func(reserve sdk.AccAddress) sdk.AccountI {
				return authtypes.NewBaseAccount(reserve, secp256k1.GenPrivKey().PubKey(), 0, 0)
			},
		},
		{
			name: "module account",
			account: func(_ sdk.AccAddress) sdk.AccountI {
				return authtypes.NewEmptyModuleAccount(ReserveModuleName(DefaultNextExchangeID))
			},
		},
	} {
		t.Run("rejects existing "+tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			reserve := f.keeper.GetReserveAddress(f.ctx, DefaultNextExchangeID)
			f.accountKeeper.SetAccount(f.ctx, tc.account(reserve))

			_, err := f.keeper.RegisterExchange(
				f.ctx,
				validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE),
			)
			require.ErrorIs(t, err, types.ErrInvariantViolation)
		})
	}
}

func TestReserveDepositorMsgServerAndGenesisRoundTrip(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	depositor, depositorAddr := testAddress(t, f.accountCodec, 0x51)
	f.accountKeeper.SetAccount(f.ctx, f.accountKeeper.NewAccountWithAddress(f.ctx, depositorAddr))
	f.bankKeeper.SetBalance(depositorAddr, sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)))

	msgServer := NewMsgServer(&f.keeper)
	_, err := msgServer.AddReserveDepositor(f.ctx, nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	_, err = msgServer.RemoveReserveDepositor(f.ctx, nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	_, err = msgServer.AddReserveDepositor(f.ctx, &types.MsgAddReserveDepositor{
		AdminAddress:     f.admin,
		ExchangeId:       exchange.GetId(),
		DepositorAddress: depositor,
	})
	require.NoError(t, err)
	_, err = msgServer.DepositReserve(f.ctx, &types.MsgDepositReserve{
		Sender:     depositor,
		ExchangeId: exchange.GetId(),
		Amount:     sdkCoinsToProto(sdk.NewCoins(sdk.NewInt64Coin("agxn", 3))),
	})
	require.NoError(t, err)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.GetReserveDepositors(), 1)
	require.Equal(t, depositor, exported.GetReserveDepositors()[0].GetDepositorAddress())

	imported := setupKeeperFixture(t)
	require.NoError(t, imported.keeper.ImportGenesis(imported.ctx, exported))
	isDepositor, err := imported.keeper.IsReserveDepositor(imported.ctx, exchange.GetId(), depositor)
	require.NoError(t, err)
	require.True(t, isDepositor)

	_, err = msgServer.RemoveReserveDepositor(f.ctx, &types.MsgRemoveReserveDepositor{
		AdminAddress:     f.admin,
		ExchangeId:       exchange.GetId(),
		DepositorAddress: depositor,
	})
	require.NoError(t, err)
}
