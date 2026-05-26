package constitution

import (
	"context"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	constitutionkeeper "github.com/gurufinglobal/guru/v3/x/constitution/keeper"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

// ConsensusVersion defines the current x/constitution module consensus version.
const ConsensusVersion = 1

var (
	_ appmodule.AppModule   = AppModule{}
	_ appmodule.HasServices = AppModule{}
	_ appmodule.HasGenesis  = AppModule{}
)

// AppModule implements x/constitution using the core appmodule extension interfaces.
type AppModule struct {
	keeper constitutionkeeper.Keeper
}

func NewAppModule(keeper constitutionkeeper.Keeper) AppModule {
	return AppModule{keeper: keeper}
}

// IsOnePerModuleType implements depinject.OnePerModuleType.
func (am AppModule) IsOnePerModuleType() {}

// IsAppModule tags this struct as a core appmodule.
func (am AppModule) IsAppModule() {}

// Name is implemented for compatibility with current app-level module helpers.
func (AppModule) Name() string {
	return constitutiontypes.ModuleName
}

// RegisterLegacyAminoCodec is intentionally empty; x/constitution is protobuf-only.
func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

// RegisterInterfaces registers interface implementations used by x/constitution messages.
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	constitutiontypes.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes wires constitution query routes into grpc-gateway.
func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := constitutionv1.RegisterQueryHandlerClient(context.Background(), mux, constitutionv1.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// RegisterServices registers Msg and Query gRPC services.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	constitutionv1.RegisterMsgServer(registrar, constitutionkeeper.NewMsgServer(&am.keeper))
	constitutionv1.RegisterQueryServer(registrar, constitutionkeeper.NewQueryServer(&am.keeper))
	return nil
}

// ConsensusVersion returns the x/constitution consensus version.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
