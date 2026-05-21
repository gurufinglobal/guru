package constitution

import (
	"context"
	"encoding/json"
	"fmt"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutionkeeper "github.com/gurufinglobal/guru/v3/x/constitution/keeper"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

// ConsensusVersion defines the current x/constitution module consensus version.
const ConsensusVersion = 1
const defaultMinValidatorBondAmount = "10"

var (
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasServices   = AppModule{}
	_ appmodule.HasGenesis    = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
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

func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.EndBlocker(ctx)
}

// DefaultGenesis writes default genesis in core GenesisTarget form.
func (AppModule) DefaultGenesis(target appmodule.GenesisTarget) error {
	return writeGenesisState(target, defaultGenesisState())
}

// ValidateGenesis validates provided genesis in core GenesisSource form.
func (AppModule) ValidateGenesis(source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source)
	if err != nil {
		return err
	}
	return validateGenesisState(genesis)
}

// InitGenesis initializes store state from genesis.
func (am AppModule) InitGenesis(ctx context.Context, source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source)
	if err != nil {
		return err
	}
	if err := validateGenesisState(genesis); err != nil {
		return err
	}
	return am.keeper.SetParams(ctx, genesis.Params)
}

// ExportGenesis writes current module state to genesis target.
func (am AppModule) ExportGenesis(ctx context.Context, target appmodule.GenesisTarget) error {
	params, err := am.keeper.GetParams(ctx)
	if err != nil {
		params = defaultGenesisState().Params
	}
	return writeGenesisState(target, &constitutionv1.GenesisState{Params: params})
}

func defaultGenesisState() *constitutionv1.GenesisState {
	return &constitutionv1.GenesisState{
		Params: &constitutionv1.Params{
			MinValidatorBondAmount: &basev1beta1.Coin{
				Denom:  appparams.BaseDenom,
				Amount: defaultMinValidatorBondAmount,
			},
		},
	}
}

func validateGenesisState(data *constitutionv1.GenesisState) error {
	if data == nil {
		return constitutiontypes.ErrInvalidParams.Wrap("genesis state cannot be nil")
	}

	return constitutionkeeper.ValidateParams(data.Params)
}

func readGenesisState(source appmodule.GenesisSource) (*constitutionv1.GenesisState, error) {
	genesis := defaultGenesisState()

	reader, err := source("params")
	if err != nil {
		return nil, fmt.Errorf("failed to read params genesis field: %w", err)
	}
	if reader == nil {
		return genesis, nil
	}
	defer reader.Close()

	params := &constitutionv1.Params{}
	if err := json.NewDecoder(reader).Decode(params); err != nil {
		return nil, fmt.Errorf("failed to decode params genesis field: %w", err)
	}

	genesis.Params = params
	return genesis, nil
}

func writeGenesisState(target appmodule.GenesisTarget, genesis *constitutionv1.GenesisState) error {
	if genesis == nil {
		return constitutiontypes.ErrInvalidParams.Wrap("genesis state cannot be nil")
	}

	writer, err := target("params")
	if err != nil {
		return fmt.Errorf("failed to open params genesis target field: %w", err)
	}
	if writer == nil {
		return fmt.Errorf("params genesis target field writer is nil")
	}

	if err := json.NewEncoder(writer).Encode(genesis.Params); err != nil {
		_ = writer.Close()
		return fmt.Errorf("failed to encode params genesis field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close params genesis field writer: %w", err)
	}

	return nil
}
