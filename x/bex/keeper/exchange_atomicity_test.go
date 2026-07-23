package keeper

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestRegisterExchangeRollsBackAllStateOnLateStoreFailure(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))

	accountKeeper := transactionalAccountKeeper{storeService: f.storeService}
	faultErr := errors.New("store fault")
	faultyKeeper := NewKeeper(
		faultStoreService{
			base: f.storeService,
			fault: &storeFault{
				op:     "set",
				prefix: types.LockedFeesKey[0],
				err:    faultErr,
			},
		},
		f.codec,
		f.accountCodec,
		accountKeeper,
		f.bankKeeper,
		f.oracleKeeper,
		f.constitutionKeeper,
		f.channelKeeper,
	)
	nextBefore, err := f.keeper.nextExchangeID.Peek(f.ctx)
	require.NoError(t, err)
	eventCount := len(f.ctx.EventManager().Events())
	exchangeID := DefaultNextExchangeID
	reserveAddress, err := faultyKeeper.GetReserveAddressString(f.ctx, exchangeID)
	require.NoError(t, err)
	reserveBytes, err := f.accountCodec.StringToBytes(reserveAddress)
	require.NoError(t, err)

	_, err = faultyKeeper.RegisterExchange(
		f.ctx,
		validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE),
	)
	require.ErrorIs(t, err, faultErr)
	require.Len(t, f.ctx.EventManager().Events(), eventCount)

	nextAfter, err := f.keeper.nextExchangeID.Peek(f.ctx)
	require.NoError(t, err)
	require.Equal(t, nextBefore, nextAfter)
	require.Nil(t, accountKeeper.GetAccount(f.ctx, sdk.AccAddress(reserveBytes)))

	_, err = f.keeper.exchanges.Get(f.ctx, exchangeID)
	require.ErrorIs(t, err, collections.ErrNotFound)
	indexed, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(f.admin, exchangeID))
	require.NoError(t, err)
	require.False(t, indexed)
	_, err = f.keeper.reserveByAddress.Get(f.ctx, reserveAddress)
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.collectedFees.Get(f.ctx, exchangeID)
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.lockedFees.Get(f.ctx, exchangeID)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestDeleteExchangeRollsBackExchangeOnIndexDeleteFailure(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	exchangeBefore := cloneExchange(exchange)
	collectedBefore, err := f.keeper.collectedFees.Get(f.ctx, exchange.GetId())
	require.NoError(t, err)
	lockedBefore, err := f.keeper.lockedFees.Get(f.ctx, exchange.GetId())
	require.NoError(t, err)
	eventCount := len(f.ctx.EventManager().Events())

	faultErr := errors.New("store fault")
	faultyKeeper := NewKeeper(
		faultStoreService{
			base: f.storeService,
			fault: &storeFault{
				op:     "delete",
				prefix: types.ExchangesByAdminKey[0],
				err:    faultErr,
			},
		},
		f.codec,
		f.accountCodec,
		f.accountKeeper,
		f.bankKeeper,
		f.oracleKeeper,
		f.constitutionKeeper,
		f.channelKeeper,
	)

	err = faultyKeeper.DeleteExchange(f.ctx, f.admin, exchange.GetId())
	require.ErrorIs(t, err, faultErr)
	require.Len(t, f.ctx.EventManager().Events(), eventCount)

	exchangeAfter, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(exchangeBefore, exchangeAfter))
	indexed, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(f.admin, exchange.GetId()))
	require.NoError(t, err)
	require.True(t, indexed)
	reserveOwner, err := f.keeper.reserveByAddress.Get(f.ctx, exchange.GetReserveAddress())
	require.NoError(t, err)
	require.Equal(t, exchange.GetId(), reserveOwner)
	collectedAfter, err := f.keeper.collectedFees.Get(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(collectedBefore, collectedAfter))
	lockedAfter, err := f.keeper.lockedFees.Get(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(lockedBefore, lockedAfter))
}

const transactionalAccountPrefix byte = 0xfe

type transactionalAccountKeeper struct {
	storeService corestore.KVStoreService
}

func (k transactionalAccountKeeper) NewAccountWithAddress(_ context.Context, address sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(address)
}

func (k transactionalAccountKeeper) GetAccount(ctx context.Context, address sdk.AccAddress) sdk.AccountI {
	value, err := k.storeService.OpenKVStore(ctx).Get(transactionalAccountKey(address))
	if err != nil {
		panic(err)
	}
	if value == nil {
		return nil
	}
	return authtypes.NewBaseAccountWithAddress(address)
}

func (k transactionalAccountKeeper) SetAccount(ctx context.Context, account sdk.AccountI) {
	if err := k.storeService.OpenKVStore(ctx).Set(transactionalAccountKey(account.GetAddress()), []byte{1}); err != nil {
		panic(err)
	}
}

func transactionalAccountKey(address sdk.AccAddress) []byte {
	key := make([]byte, len(address)+1)
	key[0] = transactionalAccountPrefix
	copy(key[1:], address)
	return key
}
