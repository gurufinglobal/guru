package app

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	evmmempool "github.com/cosmos/evm/mempool"
	cosmosevmserver "github.com/cosmos/evm/server"
)

var _ cosmosevmserver.Application = (*App)(nil)

// RegisterAPIRoutes registers SDK, CometBFT, node, and module gRPC-gateway
// routes on the operator HTTP API server.
func (app *App) RegisterAPIRoutes(apiServer *api.Server, apiConfig serverconfig.APIConfig) {
	clientCtx := apiServer.ClientCtx
	tx.RegisterGRPCGatewayRoutes(clientCtx, apiServer.GRPCGatewayRouter)
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiServer.GRPCGatewayRouter)
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, apiServer.GRPCGatewayRouter)
	app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiServer.GRPCGatewayRouter)
	if err := server.RegisterSwaggerAPI(clientCtx, apiServer.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

// RegisterTxService exposes transaction simulation, lookup, and broadcast
// support to gRPC and the Cosmos EVM JSON-RPC backend.
func (app *App) RegisterTxService(clientCtx client.Context) {
	tx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.InterfaceRegistry())
}

// RegisterTendermintService exposes CometBFT block and validator queries.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	cmtservice.RegisterTendermintService(
		clientCtx,
		app.GRPCQueryRouter(),
		app.InterfaceRegistry(),
		app.Query,
	)
}

// RegisterNodeService exposes node configuration and status queries.
func (app *App) RegisterNodeService(clientCtx client.Context, cfg serverconfig.Config) {
	nodeservice.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg)
}

// LoadHeight loads an explicitly requested committed application version.
func (app *App) LoadHeight(height int64) error {
	return app.LoadVersion(height)
}

// SetClientCtx satisfies the v0.6.2 server contract. Guru does not retain it:
// transaction broadcasting and pending queries use the server's Comet client.
func (*App) SetClientCtx(client.Context) {}

// GetMempool returns a typed nil solely for Cosmos EVM v0.6.2 server
// compatibility. Its JSON-RPC startup path asserts this concrete type without
// checking it, while the RPC backend handles a nil EVM pool. BaseApp itself is
// configured with NoOpMempool and CometBFT owns transaction storage and gossip.
func (*App) GetMempool() sdkmempool.ExtMempool {
	return (*evmmempool.ExperimentalEVMMempool)(nil)
}
