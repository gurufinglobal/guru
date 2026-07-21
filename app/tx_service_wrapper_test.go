package app

import (
	"context"
	"net"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestAppAndTxServiceKeepAminoTransactionsDisabled(t *testing.T) {
	require.Nil(t, (&App{}).LegacyAmino())

	server := blockedAminoTxServer{}
	_, err := server.TxEncodeAmino(context.Background(), &txtypes.TxEncodeAminoRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = server.TxDecodeAmino(context.Background(), &txtypes.TxDecodeAminoRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestRegisteredTxServiceEncodesProtobufAndRejectsAmino(t *testing.T) {
	config := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	clientCtx := client.Context{}.
		WithCodec(config.Codec).
		WithInterfaceRegistry(config.InterfaceRegistry).
		WithTxConfig(config.TxConfig)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	registerTxServiceNoAmino(server, clientCtx, nil, config.InterfaceRegistry)
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	txClient := txtypes.NewServiceClient(conn)
	encoded, err := txClient.TxEncode(context.Background(), &txtypes.TxEncodeRequest{Tx: &txtypes.Tx{
		Body:     &txtypes.TxBody{},
		AuthInfo: &txtypes.AuthInfo{Fee: &txtypes.Fee{}},
	}})
	require.NoError(t, err)
	require.NotEmpty(t, encoded.GetTxBytes())
	decoded, err := txClient.TxDecode(context.Background(), &txtypes.TxDecodeRequest{TxBytes: encoded.GetTxBytes()})
	require.NoError(t, err)
	require.NotNil(t, decoded.GetTx())

	_, err = txClient.TxEncodeAmino(context.Background(), &txtypes.TxEncodeAminoRequest{AminoJson: `{}`})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = txClient.TxDecodeAmino(context.Background(), &txtypes.TxDecodeAminoRequest{AminoBinary: []byte{1}})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
