package keeper

import (
	"errors"
	"testing"

	"cosmossdk.io/collections"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestUpdateAdminOnlyRotatesBEXRegistrar(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	secondBEXAdmin, _ := testAddress(t, f.accountCodec, 0x04)
	newBEXAdmin, _ := testAddress(t, f.accountCodec, 0x05)
	exchangeAdmin, _ := testAddress(t, f.accountCodec, 0x06)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, secondBEXAdmin))

	msg := validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	msg.ExchangeAdminAddress = exchangeAdmin
	exchange, err := f.keeper.RegisterExchange(f.ctx, msg)
	require.NoError(t, err)
	exchangeBefore := cloneExchange(exchange)
	eventCount := len(f.ctx.EventManager().Events())

	require.NoError(t, f.keeper.UpdateAdmin(f.ctx, f.moderator, f.admin, newBEXAdmin))

	registered, err := f.keeper.IsAdmin(f.ctx, f.admin)
	require.NoError(t, err)
	require.False(t, registered)
	registered, err = f.keeper.IsAdmin(f.ctx, newBEXAdmin)
	require.NoError(t, err)
	require.True(t, registered)
	registered, err = f.keeper.IsAdmin(f.ctx, secondBEXAdmin)
	require.NoError(t, err)
	require.True(t, registered)
	registered, err = f.keeper.IsAdmin(f.ctx, exchangeAdmin)
	require.NoError(t, err)
	require.False(t, registered)

	exchangeAfter, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(exchangeBefore, exchangeAfter))
	indexed, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(exchangeAdmin, exchange.GetId()))
	require.NoError(t, err)
	require.True(t, indexed)
	require.Len(t, f.ctx.EventManager().Events(), eventCount+1)
	require.Equal(t, types.EventTypeAdminUpdated, f.ctx.EventManager().Events()[eventCount].Type)

	oldRegistrarMsg := validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	oldRegistrarMsg.ExchangeAdminAddress = exchangeAdmin
	_, err = f.keeper.RegisterExchange(f.ctx, oldRegistrarMsg)
	require.ErrorIs(t, err, types.ErrAdminNotFound)
	newRegistrarMsg := validRegisterExchangeMsg(newBEXAdmin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	newRegistrarMsg.ExchangeAdminAddress = exchangeAdmin
	_, err = f.keeper.RegisterExchange(f.ctx, newRegistrarMsg)
	require.NoError(t, err)
	require.NoError(t, f.keeper.AssertInvariants(f.ctx))
}

func TestUpdateAdminRejectsUnsafeRegistryChanges(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	newBEXAdmin, _ := testAddress(t, f.accountCodec, 0x04)
	missingBEXAdmin, _ := testAddress(t, f.accountCodec, 0x05)
	otherModerator, _ := testAddress(t, f.accountCodec, 0x06)

	require.ErrorIs(t, f.keeper.UpdateAdmin(f.ctx, otherModerator, f.admin, newBEXAdmin), types.ErrInvalidModerator)
	require.ErrorIs(t, f.keeper.UpdateAdmin(f.ctx, f.moderator, f.admin, f.admin), types.ErrNoOpUpdate)
	require.ErrorIs(t, f.keeper.UpdateAdmin(f.ctx, f.moderator, missingBEXAdmin, newBEXAdmin), types.ErrAdminNotFound)

	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, newBEXAdmin))
	require.ErrorIs(t, f.keeper.UpdateAdmin(f.ctx, f.moderator, f.admin, newBEXAdmin), types.ErrInvalidRequest)
}

func TestUpdateAdminRollsBackRegistryOnStoreFailure(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	newBEXAdmin, _ := testAddress(t, f.accountCodec, 0x04)
	faultErr := errors.New("store fault")
	faultyKeeper := NewKeeper(
		faultStoreService{
			base: f.storeService,
			fault: &storeFault{
				op:     "set",
				prefix: types.AdminsKey[0],
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
	eventCount := len(f.ctx.EventManager().Events())

	require.ErrorIs(t, faultyKeeper.UpdateAdmin(f.ctx, f.moderator, f.admin, newBEXAdmin), faultErr)
	require.Len(t, f.ctx.EventManager().Events(), eventCount)

	registered, err := f.keeper.IsAdmin(f.ctx, f.admin)
	require.NoError(t, err)
	require.True(t, registered)
	registered, err = f.keeper.IsAdmin(f.ctx, newBEXAdmin)
	require.NoError(t, err)
	require.False(t, registered)
}

func TestRemoveBEXAdminPreservesExchangeOwnership(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)

	require.NoError(t, f.keeper.RemoveAdmin(f.ctx, f.moderator, f.admin))
	registered, err := f.keeper.IsAdmin(f.ctx, f.admin)
	require.NoError(t, err)
	require.False(t, registered)

	updated, err := f.keeper.UpdateExchange(f.ctx, f.admin, exchange.GetId(), exchange.GetRevision(), &types.ExchangeUpdatePatch{
		FeeBpsAToB: types.NewUInt32Value(exchange.GetFeeBpsAToB() + 1),
	})
	require.NoError(t, err)
	require.Equal(t, f.admin, updated.GetAdminAddress())
	require.NoError(t, f.keeper.AssertInvariants(f.ctx))
}

func TestUpdateExchangeRotatesOnlySelectedExchangeAdmin(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	oldExchangeAdmin, _ := testAddress(t, f.accountCodec, 0x04)
	newExchangeAdmin, _ := testAddress(t, f.accountCodec, 0x05)

	firstMsg := validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	firstMsg.ExchangeAdminAddress = oldExchangeAdmin
	first, err := f.keeper.RegisterExchange(f.ctx, firstMsg)
	require.NoError(t, err)
	secondMsg := validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	secondMsg.ExchangeAdminAddress = oldExchangeAdmin
	second, err := f.keeper.RegisterExchange(f.ctx, secondMsg)
	require.NoError(t, err)

	updated, err := f.keeper.UpdateExchange(f.ctx, oldExchangeAdmin, first.GetId(), first.GetRevision(), &types.ExchangeUpdatePatch{
		NewAdminAddress: types.NewStringValue(newExchangeAdmin),
	})
	require.NoError(t, err)
	require.Equal(t, newExchangeAdmin, updated.GetAdminAddress())
	require.Equal(t, first.GetRevision()+1, updated.GetRevision())

	unchanged, err := f.keeper.GetExchange(f.ctx, second.GetId())
	require.NoError(t, err)
	require.Equal(t, oldExchangeAdmin, unchanged.GetAdminAddress())
	require.Equal(t, second.GetRevision(), unchanged.GetRevision())

	oldFirstIndex, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(oldExchangeAdmin, first.GetId()))
	require.NoError(t, err)
	require.False(t, oldFirstIndex)
	newFirstIndex, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(newExchangeAdmin, first.GetId()))
	require.NoError(t, err)
	require.True(t, newFirstIndex)
	oldSecondIndex, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(oldExchangeAdmin, second.GetId()))
	require.NoError(t, err)
	require.True(t, oldSecondIndex)

	registered, err := f.keeper.IsAdmin(f.ctx, oldExchangeAdmin)
	require.NoError(t, err)
	require.False(t, registered)
	registered, err = f.keeper.IsAdmin(f.ctx, newExchangeAdmin)
	require.NoError(t, err)
	require.False(t, registered)
	require.ErrorIs(t, func() error {
		_, updateErr := f.keeper.UpdateExchange(f.ctx, oldExchangeAdmin, first.GetId(), updated.GetRevision(), &types.ExchangeUpdatePatch{
			FeeBpsAToB: types.NewUInt32Value(updated.GetFeeBpsAToB() + 1),
		})
		return updateErr
	}(), types.ErrWrongExchangeAdmin)
	_, err = f.keeper.UpdateExchange(f.ctx, newExchangeAdmin, first.GetId(), updated.GetRevision(), &types.ExchangeUpdatePatch{
		FeeBpsAToB: types.NewUInt32Value(updated.GetFeeBpsAToB() + 1),
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.AssertInvariants(f.ctx))
}

func TestUpdateExchangeAdminRotationRollsBackOnIndexFailure(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	oldExchangeAdmin, _ := testAddress(t, f.accountCodec, 0x04)
	newExchangeAdmin, _ := testAddress(t, f.accountCodec, 0x05)
	msg := validRegisterExchangeMsg(f.admin, types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE)
	msg.ExchangeAdminAddress = oldExchangeAdmin
	exchange, err := f.keeper.RegisterExchange(f.ctx, msg)
	require.NoError(t, err)
	exchangeBefore := cloneExchange(exchange)
	eventCount := len(f.ctx.EventManager().Events())

	faultErr := errors.New("store fault")
	faultyKeeper := NewKeeper(
		faultStoreService{
			base: f.storeService,
			fault: &storeFault{
				op:     "set",
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

	_, err = faultyKeeper.UpdateExchange(f.ctx, oldExchangeAdmin, exchange.GetId(), exchange.GetRevision(), &types.ExchangeUpdatePatch{
		NewAdminAddress: types.NewStringValue(newExchangeAdmin),
	})
	require.ErrorIs(t, err, faultErr)
	require.Len(t, f.ctx.EventManager().Events(), eventCount)

	exchangeAfter, err := f.keeper.GetExchange(f.ctx, exchange.GetId())
	require.NoError(t, err)
	require.True(t, types.EqualMessages(exchangeBefore, exchangeAfter))
	oldIndex, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(oldExchangeAdmin, exchange.GetId()))
	require.NoError(t, err)
	require.True(t, oldIndex)
	newIndex, err := f.keeper.exchangesByAdmin.Has(f.ctx, collections.Join(newExchangeAdmin, exchange.GetId()))
	require.NoError(t, err)
	require.False(t, newIndex)
	require.NoError(t, f.keeper.AssertInvariants(f.ctx))
}
