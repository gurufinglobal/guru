package app

import (
	"context"

	gogogrpc "github.com/cosmos/gogoproto/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
)

// blockedAminoTxServer wraps the default tx service and explicitly blocks
// legacy amino conversion endpoints for protobuf-only chains.
type blockedAminoTxServer struct {
	txtypes.ServiceServer
}

func (blockedAminoTxServer) TxEncodeAmino(context.Context, *txtypes.TxEncodeAminoRequest) (*txtypes.TxEncodeAminoResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Unsupported endpoint: TxEncodeAmino. Use TxEncode instead.")
}

func (blockedAminoTxServer) TxDecodeAmino(context.Context, *txtypes.TxDecodeAminoRequest) (*txtypes.TxDecodeAminoResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Unsupported endpoint: TxDecodeAmino. Use TxDecode instead.")
}

func registerTxServiceNoAmino(
	qrt gogogrpc.Server,
	clientCtx client.Context,
	simulateFn func([]byte) (sdk.GasInfo, *sdk.Result, error),
	interfaceRegistry codectypes.InterfaceRegistry,
) {
	base := authtx.NewTxServer(clientCtx, simulateFn, interfaceRegistry)
	txtypes.RegisterServiceServer(qrt, blockedAminoTxServer{ServiceServer: base})
}
