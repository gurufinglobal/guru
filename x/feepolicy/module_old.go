package feepolicy

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"math/rand"

// 	"github.com/grpc-ecosystem/grpc-gateway/runtime"
// 	"github.com/spf13/cobra"
// 	abci "github.com/tendermint/tendermint/abci/types"

// 	"github.com/GPTx-global/guru/x/feepolicy/client/cli"
// 	"github.com/GPTx-global/guru/x/feepolicy/keeper"
// 	"github.com/GPTx-global/guru/x/feepolicy/types"
// 	"github.com/cosmos/cosmos-sdk/client"
// 	"github.com/cosmos/cosmos-sdk/codec"
// 	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
// 	sdk "github.com/cosmos/cosmos-sdk/types"
// 	"github.com/cosmos/cosmos-sdk/types/module"
// 	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
// )

// var (
// 	_ module.AppModule           = AppModule{}
// 	_ module.AppModuleBasic      = AppModuleBasic{}
// 	_ module.AppModuleSimulation = AppModule{}
// )

// // AppModuleBasic defines the basic application module used by the feepolicy module.
// type AppModuleBasic struct{}

// var _ module.AppModuleBasic = AppModuleBasic{}

// // Name returns the feepolicy module's name.
// func (AppModuleBasic) Name() string {
// 	return types.ModuleName
// }

// // RegisterLegacyAminoCodec registers the feepolicy module's types on the given LegacyAmino codec.
// func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
// 	types.RegisterLegacyAminoCodec(cdc)
// }

// // ConsensusVersion returns the consensus state-breaking version for the module.
// func (AppModuleBasic) ConsensusVersion() uint64 { return 1 }

// // RegisterInterfaces registers the module's interface types
// func (b AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
// 	types.RegisterInterfaces(registry)
// }

// // DefaultGenesis returns default genesis state as raw bytes for the feepolicy
// // module.
// func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
// 	return cdc.MustMarshalJSON(types.DefaultGenesisState())
// }

// // ValidateGenesis performs genesis state validation for the feepolicy module.
// func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
// 	var genesisState types.GenesisState
// 	if err := cdc.UnmarshalJSON(bz, &genesisState); err != nil {
// 		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
// 	}

// 	return genesisState.Validate()
// }

// // RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the feepolicy module.
// func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
// 	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
// 		panic(err)
// 	}
// }

// // GetTxCmd returns no root tx command for the feepolicy module.
// func (AppModuleBasic) GetTxCmd() *cobra.Command {
// 	return cli.GetTxCmd()
// }

// // GetQueryCmd returns the root query command for the feepolicy module.
// func (AppModuleBasic) GetQueryCmd() *cobra.Command {
// 	return cli.GetQueryCmd()
// }

// // AppModule implements an application module for the feepolicy module.
// type AppModule struct {
// 	AppModuleBasic

// 	keeper keeper.Keeper
// }

// // NewAppModule creates a new AppModule object
// func NewAppModule(cdc codec.Codec, keeper keeper.Keeper) AppModule {

// 	return AppModule{
// 		AppModuleBasic: AppModuleBasic{},
// 		keeper:         keeper,
// 	}
// }

// // Name returns the feepolicy module's name.
// func (AppModule) Name() string {
// 	return types.ModuleName
// }

// // RegisterInvariants registers the feepolicy module invariants.
// func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// // func (am AppModule) NewHandler() sdk.Handler {
// // 	return NewHandler(&am.keeper)
// // }

// // // Route returns the message routing key for the feepolicy module.
// // func (am AppModule) Route() sdk.Route {
// // 	return sdk.NewRoute(types.RouterKey, am.NewHandler())
// // }

// // QuerierRoute returns the feepolicy module's querier route name.
// func (AppModule) QuerierRoute() string {
// 	return types.RouterKey
// }

// // LegacyQuerierHandler returns the feepolicy module sdk.Querier.
// func (am AppModule) LegacyQuerierHandler(legacyQuerierCdc *codec.LegacyAmino) sdk.Querier {
// 	return nil
// }

// // RegisterServices registers a gRPC query service to respond to the
// // module-specific gRPC queries.
// func (am AppModule) RegisterServices(cfg module.Configurator) {
// 	types.RegisterMsgServer(cfg.MsgServer(), &am.keeper)
// 	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)

// 	// migration registration will be here in case of verion upgrade
// }

// // BeginBlock returns the begin blocker for the feepolicy module.
// func (am AppModule) BeginBlock(ctx sdk.Context, _ abci.RequestBeginBlock) {
// }

// // EndBlock returns the end blocker for the feepolicy module.
// func (am AppModule) EndBlock(_ sdk.Context, _ abci.RequestEndBlock) []abci.ValidatorUpdate {
// 	return []abci.ValidatorUpdate{}
// }

// // InitGenesis performs genesis initialization for the feepolicy module. It returns
// // no validator updates.
// func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
// 	var genesisState types.GenesisState
// 	cdc.MustUnmarshalJSON(data, &genesisState)
// 	InitGenesis(ctx, am.keeper, genesisState)

// 	return []abci.ValidatorUpdate{}
// }

// // ExportGenesis returns the exported genesis state as raw bytes for the feepolicy module.
// func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
// 	gs := ExportGenesis(ctx, am.keeper)
// 	return cdc.MustMarshalJSON(&gs)
// }

// // GenerateGenesisState creates a randomized GenState of the feepolicy module.
// func (AppModule) GenerateGenesisState(_ *module.SimulationState) {
// }

// // ProposalContents doesn't return any content functions for governance proposals.
// func (AppModule) ProposalContents(simState module.SimulationState) []simtypes.WeightedProposalContent {
// 	return nil
// }

// // // RandomizedParams creates randomized feepolicy param changes for the simulator.
// // func (AppModule) RandomizedParams(r *rand.Rand) []simtypes.ParamChange {
// // 	return []simtypes.ParamChange{}
// // }

// // // RegisterStoreDecoder registers a decoder for feepolicy module's types.
// // func (am AppModule) RegisterStoreDecoder(sdr sdk.StoreDecoderRegistry) {
// // }

// // WeightedOperations doesn't return any cex module operation.
// func (AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
// 	return nil
// }
