package feepolicy

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/keeper"
	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

const ConsensusVersion = 1

var (
	_ appmodule.AppModule   = AppModule{}
	_ appmodule.HasServices = AppModule{}
	_ appmodule.HasGenesis  = AppModule{}
)

type AppModule struct {
	keeper keeper.Keeper
}

func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{keeper: k}
}

func (AppModule) IsOnePerModuleType() {}

func (AppModule) IsAppModule() {}

func (AppModule) Name() string { return types.ModuleName }

// Feepolicy is protobuf-only; legacy Amino registration remains a no-op.
func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	types.RegisterMsgServer(registrar, keeper.NewMsgServer(&am.keeper))
	types.RegisterQueryServer(registrar, keeper.NewQueryServer(&am.keeper))
	return nil
}

func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
