package example

import (
	"context"
	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/gurufinglobal/guru/v3/x/example/keeper"
	"github.com/gurufinglobal/guru/v3/x/example/types"
)

const ConsensusVersion = 1

var (
	_ module.AppModuleBasic      = AppModuleBasic{}
	_ module.HasGenesisBasics    = AppModuleBasic{}
	_ module.AppModule           = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasServices         = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ appmodule.AppModule        = AppModule{}
)

// AppModuleBasic은 상태 의존성이 없는 모듈 기본 기능(인터페이스/제네시스 인코딩)을 제공한다.
type AppModuleBasic struct {
	cdc codec.Codec
}

func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec:
// 본 체인은 proto-only 운영 정책이므로 legacy amino 경로를 사용하지 않는다.
// 단, SDK의 AppModuleBasic 계약을 만족하기 위해 no-op으로 구현한다.
func (AppModuleBasic) RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

func (AppModuleBasic) RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registrar)
}

func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *gwruntime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var data types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis: %w", types.ModuleName, err)
	}
	return types.ValidateGenesis(data)
}

// AppModule은 keeper를 포함한 상태 기반 모듈 구현체다.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

func NewAppModule(cdc codec.Codec, k keeper.Keeper) AppModule {
	return AppModule{
		AppModuleBasic: AppModuleBasic{cdc: cdc},
		keeper:         k,
	}
}

// IsOnePerModuleType/IsAppModule은 depinject + core appmodule 태그 메서드다.
func (AppModule) IsOnePerModuleType() {}
func (AppModule) IsAppModule()        {}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	var genesisState types.GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)
	if err := am.keeper.InitGenesis(ctx, &genesisState); err != nil {
		panic(fmt.Errorf("x/example init genesis failed: %w", err))
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	state, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("x/example export genesis failed: %w", err))
	}
	return cdc.MustMarshalJSON(state)
}

func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
