package transwap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/client/cli"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/keeper"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/simulation"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

var (
	_ module.AppModuleBasic      = AppModuleBasic{}
	_ module.HasName             = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ appmodule.AppModule        = AppModule{}
	_ appmodule.HasServices      = AppModule{}
	_ appmodule.HasGenesis       = AppModule{}
	_ appmodule.HasEndBlocker    = AppModule{}

	_ porttypes.IBCModule = (*IBCModule)(nil)
)

// AppModuleBasic is the IBC Transfer AppModuleBasic
type AppModuleBasic struct{}

// Name implements AppModuleBasic interface
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (AppModule) IsOnePerModuleType() {}

// IsAppModule implements the appmodule.AppModule interface.
func (AppModule) IsAppModule() {}

// RegisterLegacyAminoCodec implements AppModuleBasic interface
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers module concrete types into protobuf Any.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state as raw bytes for the ibc
// transfer module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis performs genesis state validation for the ibc transfer module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var gs transwapv1.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}

	return types.ValidateGenesisState(&gs)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the transwap module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := transwapv1.RegisterQueryHandlerClient(context.Background(), mux, transwapv1.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetTxCmd implements AppModuleBasic interface
func (AppModuleBasic) GetTxCmd() *cobra.Command {
	return nil
}

// GetQueryCmd implements AppModuleBasic interface
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return cli.GetQueryCmd()
}

// AppModule represents the AppModule for this module
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

// NewAppModule creates a new 20-transfer module
func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{
		keeper: k,
	}
}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	transwapv1.RegisterMsgServer(registrar, keeper.NewMsgServer(&am.keeper))
	transwapv1.RegisterQueryServer(registrar, am.keeper)
	return nil
}

// EndBlock advances a bounded number of persisted local refund dispatch
// failures. IBC timeout/ack retries still happen immediately in their callback;
// only failures that emitted no packet enter this queue.
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.ProcessRefundRetryQueue(sdk.UnwrapSDKContext(ctx))
}

// DefaultGenesis writes the default genesis state using the SDK 0.54 core
// appmodule genesis target API.
func (am AppModule) DefaultGenesis(target appmodule.GenesisTarget) error {
	return writeGenesisState(target, types.DefaultGenesisState())
}

// ValidateGenesis validates genesis using the SDK 0.54 core appmodule genesis
// source API.
func (am AppModule) ValidateGenesis(source appmodule.GenesisSource) error {
	genesisState, err := readGenesisState(source, types.DefaultGenesisState())
	if err != nil {
		return err
	}
	return types.ValidateGenesisState(genesisState)
}

// InitGenesis performs genesis initialization for the ibc-transfer module.
func (am AppModule) InitGenesis(ctx context.Context, source appmodule.GenesisSource) error {
	genesisState, err := readGenesisState(source, types.DefaultGenesisState())
	if err != nil {
		return err
	}
	if err := types.ValidateGenesisState(genesisState); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	am.keeper.InitGenesis(sdkCtx, genesisState)
	return am.keeper.AssertRefundInvariants(sdkCtx)
}

// ExportGenesis exports genesis using the SDK 0.54 core appmodule genesis
// target API.
func (am AppModule) ExportGenesis(ctx context.Context, target appmodule.GenesisTarget) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := am.keeper.AssertRefundInvariants(sdkCtx); err != nil {
		return err
	}
	gs := am.keeper.ExportGenesis(sdkCtx)
	return writeGenesisState(target, gs)
}

func readGenesisState(source appmodule.GenesisSource, defaults *transwapv1.GenesisState) (*transwapv1.GenesisState, error) {
	genesis := &transwapv1.GenesisState{
		PortId:        defaults.GetPortId(),
		Denoms:        defaults.GetDenoms(),
		TotalEscrowed: defaults.GetTotalEscrowed(),
		Params:        defaults.GetParams(),
		Refunds:       defaults.GetRefunds(),
	}

	for _, fieldName := range []string{
		"port_id",
		"denoms",
		"total_escrowed",
		"params",
		"refunds",
	} {
		partial, found, err := readGenesisStateField(source, fieldName)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		switch fieldName {
		case "port_id":
			genesis.PortId = partial.GetPortId()
		case "denoms":
			genesis.Denoms = partial.GetDenoms()
		case "total_escrowed":
			genesis.TotalEscrowed = partial.GetTotalEscrowed()
		case "params":
			genesis.Params = partial.GetParams()
		case "refunds":
			genesis.Refunds = partial.GetRefunds()
		}
	}

	return genesis, nil
}

func writeGenesisState(target appmodule.GenesisTarget, genesis *transwapv1.GenesisState) error {
	if genesis == nil {
		return types.ErrDecodeGenesisField.Wrap("genesis state cannot be nil")
	}

	if err := writeGenesisField(target, "port_id", genesis.GetPortId()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "denoms", genesis.GetDenoms()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "total_escrowed", genesis.GetTotalEscrowed()); err != nil {
		return err
	}
	if err := writeGenesisField(target, "params", genesis.GetParams()); err != nil {
		return err
	}
	return writeGenesisField(target, "refunds", genesis.GetRefunds())
}

func readGenesisStateField(
	source appmodule.GenesisSource,
	fieldName string,
) (*transwapv1.GenesisState, bool, error) {
	reader, err := source(fieldName)
	if err != nil {
		return nil, false, types.ErrReadGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	if reader == nil {
		return nil, false, nil
	}
	defer func() { _ = reader.Close() }()

	fieldJSON, err := io.ReadAll(reader)
	if err != nil {
		return nil, false, types.ErrReadGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	fieldNameJSON, err := json.Marshal(fieldName)
	if err != nil {
		return nil, false, types.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	wrapped := make([]byte, 0, len(fieldNameJSON)+len(fieldJSON)+3)
	wrapped = append(wrapped, '{')
	wrapped = append(wrapped, fieldNameJSON...)
	wrapped = append(wrapped, ':')
	wrapped = append(wrapped, fieldJSON...)
	wrapped = append(wrapped, '}')

	partial := &transwapv1.GenesisState{}
	if err := protojson.Unmarshal(wrapped, partial); err != nil {
		return nil, false, types.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}

	return partial, true, nil
}

func writeGenesisField(target appmodule.GenesisTarget, fieldName string, value any) error {
	writer, err := target(fieldName)
	if err != nil {
		return types.ErrOpenGenesisTargetField.Wrapf("%s: %v", fieldName, err)
	}
	if writer == nil {
		return types.ErrNilGenesisTargetWriter.Wrapf("%s genesis target field writer is nil", fieldName)
	}

	if err := json.NewEncoder(writer).Encode(value); err != nil {
		_ = writer.Close()
		return types.ErrEncodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}

	if err := writer.Close(); err != nil {
		return types.ErrCloseGenesisFieldWriter.Wrapf("%s: %v", fieldName, err)
	}

	return nil
}

// ConsensusVersion implements AppModule/ConsensusVersion defining the current version of transfer.
func (AppModule) ConsensusVersion() uint64 { return 6 }

// AppModuleSimulation functions

// GenerateGenesisState creates a randomized GenState of the transfer module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return nil
}

// RegisterStoreDecoder registers a decoder for transfer module's types
func (AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simulation.NewDecodeStore()
}

// WeightedOperations returns the all the transfer module operations with their respective weights.
func (AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
