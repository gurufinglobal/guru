package cosmos_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	cosmosante "github.com/gurufinglobal/guru/v2/ante/cosmos"
	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	transwaptypes "github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

func TestRecoverClientGrantDecorator_RejectsNonGovGranter(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	// valid IBC path in state
	portID := transwaptypes.ModuleName
	channelID := testChannelID
	connID := testConnID
	nw.App.IBCKeeper.ChannelKeeper.SetChannel(ctx, portID, channelID, channeltypes.Channel{ConnectionHops: []string{connID}})
	nw.App.IBCKeeper.ConnectionKeeper.SetConnection(ctx, connID, connectiontypes.ConnectionEnd{ClientId: testClientID})

	decorator := cosmosante.NewRecoverClientGrantDecorator(nw.App.IBCKeeper)

	nonGov := sdk.AccAddress([]byte("non_gov_addr________"))[:20]
	grantee := sdk.AccAddress([]byte("grantee_addr________"))[:20]
	exp := ctx.BlockTime().Add(24 * time.Hour)
	auth := &transwaptypes.RecoverClientAuthorization{
		MsgTypeUrl: transwaptypes.MsgTypeURLRecoverClient,
		AllowedPaths: []transwaptypes.AllowedPath{
			{PortId: portID, ChannelId: channelID},
		},
	}
	msgGrant := newMsgGrant(nonGov, grantee, auth, &exp)

	_, err := decorator.AnteHandle(ctx, testTx{msgs: []sdk.Msg{msgGrant}}, false, nextNoop)
	require.Error(t, err)
	require.Contains(t, err.Error(), "granter must be GOV_ADDR")
}

func TestRecoverClientGrantDecorator_AllowsGovGranterWithResolvablePaths(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	portID := transwaptypes.ModuleName
	channelID := testChannelID
	connID := testConnID
	nw.App.IBCKeeper.ChannelKeeper.SetChannel(ctx, portID, channelID, channeltypes.Channel{ConnectionHops: []string{connID}})
	nw.App.IBCKeeper.ConnectionKeeper.SetConnection(ctx, connID, connectiontypes.ConnectionEnd{ClientId: testClientID})

	decorator := cosmosante.NewRecoverClientGrantDecorator(nw.App.IBCKeeper)

	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	grantee := sdk.AccAddress([]byte("grantee_addr________"))[:20]
	exp := ctx.BlockTime().Add(24 * time.Hour)
	auth := &transwaptypes.RecoverClientAuthorization{
		MsgTypeUrl: transwaptypes.MsgTypeURLRecoverClient,
		AllowedPaths: []transwaptypes.AllowedPath{
			{PortId: portID, ChannelId: channelID},
		},
	}
	msgGrant := newMsgGrant(govAddr, grantee, auth, &exp)

	_, err := decorator.AnteHandle(ctx, testTx{msgs: []sdk.Msg{msgGrant}}, false, nextNoop)
	require.NoError(t, err)
}
