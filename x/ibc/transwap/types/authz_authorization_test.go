package types_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"

	transwaptypes "github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

// This test guarantees "Phase 1": the custom authorization can be packed into Any,
// stored in MsgGrant, and unpacked back as authz.Authorization.
//
// NOTE: This requires the generated file from proto-gen (authz.pb.go) to exist.
func TestRecoverClientAuthorization_MsgGrantPackUnpack(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	authz.RegisterInterfaces(registry)
	transwaptypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	granter := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	grantee := sdk.AccAddress(bytes.Repeat([]byte{0x02}, 20))
	exp := time.Now().Add(1 * time.Hour)

	authorization := &transwaptypes.RecoverClientAuthorization{
		MsgTypeUrl: transwaptypes.MsgTypeURLRecoverClient,
		AllowedPaths: []transwaptypes.AllowedPath{
			{PortId: "transwap", ChannelId: "channel-0"},
		},
	}

	msg, err := authz.NewMsgGrant(granter, grantee, authorization, &exp)
	require.NoError(t, err)

	bz, err := cdc.Marshal(msg)
	require.NoError(t, err)

	var decoded authz.MsgGrant
	require.NoError(t, cdc.Unmarshal(bz, &decoded))
	require.NoError(t, decoded.UnpackInterfaces(cdc))

	gotAuth, err := decoded.GetAuthorization()
	require.NoError(t, err)
	require.IsType(t, &transwaptypes.RecoverClientAuthorization{}, gotAuth)
	require.NoError(t, gotAuth.ValidateBasic())
}
