package cosmos_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	ibcclienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	cosmosante "github.com/gurufinglobal/guru/v2/ante/cosmos"
	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	transwaptypes "github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

func TestRecoverClientDecorator_ExpiredGrantRejected(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	// Set up IBC state: (transwap, channel-0) -> connection-0 -> client-0
	portID := transwaptypes.ModuleName
	channelID := testChannelID
	connID := testConnID
	clientID := testClientID

	nw.App.IBCKeeper.ChannelKeeper.SetChannel(ctx, portID, channelID, channeltypes.Channel{
		ConnectionHops: []string{connID},
	})
	nw.App.IBCKeeper.ConnectionKeeper.SetConnection(ctx, connID, connectiontypes.ConnectionEnd{
		ClientId: clientID,
	})

	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	grantee := sdk.AccAddress([]byte("grantee_addr________"))[:20]

	// Store a grant that will later become expired by moving block time forward.
	exp := ctx.BlockTime().Add(1 * time.Hour)
	auth := &transwaptypes.RecoverClientAuthorization{
		MsgTypeUrl: transwaptypes.MsgTypeURLRecoverClient,
		AllowedPaths: []transwaptypes.AllowedPath{
			{PortId: portID, ChannelId: channelID},
		},
	}
	require.NoError(t, nw.App.AuthzKeeper.SaveGrant(ctx, grantee, govAddr, auth, &exp))

	decorator := cosmosante.NewRecoverClientDecorator(nw.App.IBCKeeper, &nw.App.AuthzKeeper)

	rc := &ibcclienttypes.MsgRecoverClient{
		SubjectClientId:    clientID,
		SubstituteClientId: "07-tendermint-9",
		Signer:             govAddr.String(),
	}
	exec := newMsgExec(grantee, []sdk.Msg{rc})

	ctxAfter := ctx.WithBlockTime(exp.Add(1 * time.Second))
	_, err := decorator.AnteHandle(ctxAfter, testTx{msgs: []sdk.Msg{exec}}, false, nextNoop)
	require.Error(t, err)
	// AuthzKeeper.GetAuthorization returns nil for expired grants, so the decorator sees it as missing.
	require.Contains(t, err.Error(), "missing authz grant")
}

func TestRecoverClientDecorator_SubjectOutOfScopeRejected(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	portID := transwaptypes.ModuleName
	channelID := testChannelID
	connID := testConnID
	allowedClientID := testClientID

	nw.App.IBCKeeper.ChannelKeeper.SetChannel(ctx, portID, channelID, channeltypes.Channel{
		ConnectionHops: []string{connID},
	})
	nw.App.IBCKeeper.ConnectionKeeper.SetConnection(ctx, connID, connectiontypes.ConnectionEnd{
		ClientId: allowedClientID,
	})

	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	grantee := sdk.AccAddress([]byte("grantee_addr________"))[:20]

	exp := ctx.BlockTime().Add(24 * time.Hour)
	auth := &transwaptypes.RecoverClientAuthorization{
		MsgTypeUrl: transwaptypes.MsgTypeURLRecoverClient,
		AllowedPaths: []transwaptypes.AllowedPath{
			{PortId: portID, ChannelId: channelID},
		},
	}
	require.NoError(t, nw.App.AuthzKeeper.SaveGrant(ctx, grantee, govAddr, auth, &exp))

	decorator := cosmosante.NewRecoverClientDecorator(nw.App.IBCKeeper, &nw.App.AuthzKeeper)

	rc := &ibcclienttypes.MsgRecoverClient{
		SubjectClientId:    "07-tendermint-999",
		SubstituteClientId: "07-tendermint-9",
		Signer:             govAddr.String(),
	}
	exec := newMsgExec(grantee, []sdk.Msg{rc})

	_, err := decorator.AnteHandle(ctx, testTx{msgs: []sdk.Msg{exec}}, false, nextNoop)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of scope")
}

func TestRecoverClientDecorator_AllowsInScope(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	portID := transwaptypes.ModuleName
	channelID := testChannelID
	connID := testConnID
	clientID := testClientID

	nw.App.IBCKeeper.ChannelKeeper.SetChannel(ctx, portID, channelID, channeltypes.Channel{
		ConnectionHops: []string{connID},
	})
	nw.App.IBCKeeper.ConnectionKeeper.SetConnection(ctx, connID, connectiontypes.ConnectionEnd{
		ClientId: clientID,
	})

	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	grantee := sdk.AccAddress([]byte("grantee_addr________"))[:20]

	exp := ctx.BlockTime().Add(24 * time.Hour)
	auth := &transwaptypes.RecoverClientAuthorization{
		MsgTypeUrl: transwaptypes.MsgTypeURLRecoverClient,
		AllowedPaths: []transwaptypes.AllowedPath{
			{PortId: portID, ChannelId: channelID},
		},
	}
	require.NoError(t, nw.App.AuthzKeeper.SaveGrant(ctx, grantee, govAddr, auth, &exp))

	decorator := cosmosante.NewRecoverClientDecorator(nw.App.IBCKeeper, &nw.App.AuthzKeeper)

	rc := &ibcclienttypes.MsgRecoverClient{
		SubjectClientId:    clientID,
		SubstituteClientId: "07-tendermint-9",
		Signer:             govAddr.String(),
	}
	exec := newMsgExec(grantee, []sdk.Msg{rc})

	_, err := decorator.AnteHandle(ctx, testTx{msgs: []sdk.Msg{exec}}, false, nextNoop)
	require.NoError(t, err)
}

// Minimal Tx implementation for direct ante decorator tests.
type testTx struct{ msgs []sdk.Msg }

func (t testTx) GetMsgs() []sdk.Msg { return t.msgs }

func (t testTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func nextNoop(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) { return ctx, nil }
