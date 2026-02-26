package feeproxy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gorilla/mux"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/client/cli"
	"github.com/gurufinglobal/guru/v2/x/feeproxy/keeper"
	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

// consensusVersion defines the current x/feeproxy module consensus version.
const consensusVersion = 1

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}

	_ module.HasABCIGenesis = AppModule{}
	_ appmodule.AppModule   = AppModule{}
)

// AppModuleBasic defines the basic application module used by x/feeproxy.
type AppModuleBasic struct{}

func (AppModuleBasic) Name() string { return types.ModuleName }

func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

func (AppModuleBasic) RegisterInterfaces(reg codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(reg)
}

func (AppModuleBasic) RegisterGRPCGatewayRoutes(c client.Context, serveMux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), serveMux, types.NewQueryClient(c)); err != nil {
		panic(err)
	}
}

func (AppModuleBasic) ConsensusVersion() uint64 { return consensusVersion }

func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {}

func (AppModuleBasic) GetTxCmd() *cobra.Command { return cli.GetTxCmd() }

func (AppModuleBasic) GetQueryCmd() *cobra.Command { return cli.GetQueryCmd() }

// AppModule implements an application module for x/feeproxy.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

func (AppModule) Name() string { return types.ModuleName }

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)
	types.RegisterMsgServer(cfg.MsgServer(), &am.keeper)
}

func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	var gs types.GenesisState
	cdc.MustUnmarshalJSON(data, &gs)
	InitGenesis(ctx, am.keeper, gs)
	return nil
}

func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs := ExportGenesis(ctx, am.keeper)
	return cdc.MustMarshalJSON(&gs)
}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

// RegisterStoreDecoder registers a decoder for feeproxy module's types.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// GenerateGenesisState creates a randomized GenState of the feeproxy module.
func (AppModule) GenerateGenesisState(_ *module.SimulationState) {}

// WeightedOperations returns the module operations with their respective weights.
func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation { return nil }

