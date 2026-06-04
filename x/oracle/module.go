package oracle

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

const ConsensusVersion = 1

var (
	_ appmodule.AppModule   = AppModule{}
	_ appmodule.HasServices = AppModule{}
	_ appmodule.HasGenesis  = AppModule{}
)

type AppModule struct {
	keeper oraclekeeper.Keeper
}

func NewAppModule(keeper oraclekeeper.Keeper) AppModule {
	return AppModule{keeper: keeper}
}

func (am AppModule) IsOnePerModuleType() {}

func (am AppModule) IsAppModule() {}

func (AppModule) Name() string {
	return oracletypes.ModuleName
}

func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	oracletypes.RegisterInterfaces(registry)
}

func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := oraclev1.RegisterQueryHandlerClient(context.Background(), mux, oraclev1.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	oraclev1.RegisterMsgServer(registrar, oraclekeeper.NewMsgServer(&am.keeper))
	oraclev1.RegisterQueryServer(registrar, oraclekeeper.NewQueryServer(&am.keeper))
	return nil
}

func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
