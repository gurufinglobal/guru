package constitution

import (
	"context"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	constitutionkeeper "github.com/gurufinglobal/guru/v2/x/constitution/keeper"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
)

// ConsensusVersion defines the current x/constitution module consensus version.
const ConsensusVersion = 2

var (
	_ appmodule.AppModule       = AppModule{}
	_ appmodule.HasServices     = AppModule{}
	_ appmodule.HasGenesis      = AppModule{}
	_ appmodule.HasBeginBlocker = AppModule{}
	_ appmodule.HasEndBlocker   = AppModule{}
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
	if err := constitutiontypes.RegisterQueryHandlerClient(context.Background(), mux, constitutiontypes.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// RegisterServices registers Msg and Query gRPC services.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	constitutiontypes.RegisterMsgServer(registrar, constitutionkeeper.NewMsgServer(&am.keeper))
	constitutiontypes.RegisterQueryServer(registrar, constitutionkeeper.NewQueryServer(&am.keeper))
	return nil
}

// BeginBlock executes fee separation before distribution observes the fee
// collector. This keeps the chain policy module as the source of truth for
// base/burn/validator fee routing.
func (am AppModule) BeginBlock(ctx context.Context) error {
	return am.keeper.ExecuteSeparation(ctx)
}

// EndBlock applies a due oracle-driven minimum gas price schedule after normal
// tx execution. The updated feemarket parameter is therefore a next-block
// admission rule, which avoids changing the fee rule mid-block.
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.ApplyDueMinGasPriceSchedule(ctx)
}

// ConsensusVersion returns the x/constitution consensus version.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
