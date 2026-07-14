package keeper

import (
	"testing"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestMsgServerRoutesUpdateAdmin(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	server := NewMsgServer(&f.keeper)

	_, err := server.UpdateAdmin(f.ctx, nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	wrongModerator, _ := testAddress(t, f.accountCodec, 0x60)
	newBEXAdmin, _ := testAddress(t, f.accountCodec, 0x61)
	_, err = server.UpdateAdmin(f.ctx, &bexv1.MsgUpdateAdmin{
		Moderator:       wrongModerator,
		OldAdminAddress: f.admin,
		NewAdminAddress: newBEXAdmin,
	})
	require.ErrorIs(t, err, types.ErrInvalidModerator)

	updateResponse, err := server.UpdateAdmin(f.ctx, &bexv1.MsgUpdateAdmin{
		Moderator:       f.moderator,
		OldAdminAddress: f.admin,
		NewAdminAddress: newBEXAdmin,
	})
	require.NoError(t, err)
	require.NotNil(t, updateResponse)
	registered, err := f.keeper.IsAdmin(f.ctx, f.admin)
	require.NoError(t, err)
	require.False(t, registered)
	registered, err = f.keeper.IsAdmin(f.ctx, newBEXAdmin)
	require.NoError(t, err)
	require.True(t, registered)
}
