package bex

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

const ConsensusVersion = 1

var (
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasServices   = AppModule{}
	_ appmodule.HasGenesis    = AppModule{}
	_ sdkmodule.HasInvariants = AppModule{}

	registerQueryGateway = bexv1.RegisterQueryHandlerClient
)

type AppModule struct {
	keeper bexkeeper.Keeper
}

func NewAppModule(keeper bexkeeper.Keeper) AppModule {
	return AppModule{keeper: keeper}
}

func (am AppModule) IsOnePerModuleType() {}

func (am AppModule) IsAppModule() {}

func (AppModule) Name() string {
	return types.ModuleName
}

func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := registerQueryGateway(context.Background(), mux, bexv1.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	bexv1.RegisterMsgServer(registrar, bexkeeper.NewMsgServer(&am.keeper))
	bexv1.RegisterQueryServer(registrar, bexkeeper.NewQueryServer(&am.keeper))
	return nil
}

func (am AppModule) RegisterInvariants(registry sdk.InvariantRegistry) {
	bexkeeper.RegisterInvariants(registry, am.keeper)
}

func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
