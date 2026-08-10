package app

import (
	"context"
	"encoding/json"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/module"
	precompiletypes "github.com/cosmos/evm/precompiles/types"
	evmmodule "github.com/cosmos/evm/x/vm"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	ethvm "github.com/ethereum/go-ethereum/core/vm"

	"github.com/gurufinglobal/guru/v2/config"
)

const vmMsgServiceName = "cosmos.evm.vm.v1.Msg"

type installedStaticPrecompiles map[common.Address]struct{}

func stageCStaticPrecompiles() map[common.Address]ethvm.PrecompiledContract {
	return precompiletypes.NewStaticPrecompiles().WithPraguePrecompiles()
}

func snapshotStaticPrecompiles(contracts map[common.Address]ethvm.PrecompiledContract) installedStaticPrecompiles {
	installed := make(installedStaticPrecompiles, len(contracts))
	for address := range contracts {
		installed[address] = struct{}{}
	}
	return installed
}

func validateActiveStaticPrecompiles(active []string, installed installedStaticPrecompiles) error {
	for _, rawAddress := range active {
		if !common.IsHexAddress(rawAddress) {
			return fmt.Errorf("active static precompile %q is not a valid hex address", rawAddress)
		}
		address := common.HexToAddress(rawAddress)
		if _, ok := installed[address]; !ok {
			return fmt.Errorf("active static precompile %q has no installed implementation", rawAddress)
		}
	}
	return nil
}

func validateVMParameterPolicy(params evmtypes.Params, installed installedStaticPrecompiles) error {
	if params.EvmDenom != config.BaseDenom {
		return fmt.Errorf("EVM denom must be %q, got %q", config.BaseDenom, params.EvmDenom)
	}
	if params.ExtendedDenomOptions == nil ||
		params.ExtendedDenomOptions.ExtendedDenom != config.BaseDenom {
		return fmt.Errorf("EVM extended denom must be %q", config.BaseDenom)
	}
	return validateActiveStaticPrecompiles(params.ActiveStaticPrecompiles, installed)
}

func validateVMGenesisPolicy(genesis evmtypes.GenesisState, installed installedStaticPrecompiles) error {
	if len(genesis.Preinstalls) != 0 {
		return fmt.Errorf("EVM genesis preinstalls are not enabled in the Stage C application")
	}
	if err := validateVMParameterPolicy(genesis.Params, installed); err != nil {
		return err
	}
	return validateVMGenesisAccountEncoding(&genesis)
}

type guardedVMMsgServer struct {
	evmtypes.MsgServer
	installed installedStaticPrecompiles
}

var _ evmtypes.MsgServer = (*guardedVMMsgServer)(nil)

func (server *guardedVMMsgServer) UpdateParams(
	ctx context.Context,
	req *evmtypes.MsgUpdateParams,
) (*evmtypes.MsgUpdateParamsResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "VM parameter update request cannot be nil")
	}
	if err := req.Params.Validate(); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	if err := validateVMParameterPolicy(req.Params, server.installed); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	return server.MsgServer.UpdateParams(ctx, req)
}

func (*guardedVMMsgServer) RegisterPreinstalls(
	context.Context,
	*evmtypes.MsgRegisterPreinstalls,
) (*evmtypes.MsgRegisterPreinstallsResponse, error) {
	return nil, errorsmod.Wrap(
		sdkerrors.ErrNotSupported,
		"EVM preinstall registration is not enabled in the Stage C application",
	)
}

// guardedVMAppModule keeps the upstream VM lifecycle while replacing only the
// parameter-update service and the two genesis entry points with registry
// completeness checks.
type guardedVMAppModule struct {
	evmmodule.AppModule
	installed installedStaticPrecompiles
}

var (
	_ module.AppModule      = guardedVMAppModule{}
	_ module.HasServices    = guardedVMAppModule{}
	_ module.HasABCIGenesis = guardedVMAppModule{}
)

func newGuardedVMAppModule(
	upstream evmmodule.AppModule,
	installed installedStaticPrecompiles,
) guardedVMAppModule {
	return guardedVMAppModule{
		AppModule: upstream,
		installed: installed,
	}
}

func (appModule guardedVMAppModule) RegisterServices(cfg module.Configurator) {
	registerServicesWithMsgServerGuard(
		cfg,
		vmMsgServiceName,
		func(implementation any) any {
			server, ok := implementation.(evmtypes.MsgServer)
			if !ok {
				panic(fmt.Errorf("unexpected VM MsgServer implementation %T", implementation))
			}
			return &guardedVMMsgServer{MsgServer: server, installed: appModule.installed}
		},
		appModule.AppModule.RegisterServices,
	)
}

func (appModule guardedVMAppModule) ValidateGenesis(
	cod codec.JSONCodec,
	txConfig client.TxEncodingConfig,
	raw json.RawMessage,
) error {
	if err := appModule.AppModule.ValidateGenesis(cod, txConfig, raw); err != nil {
		return err
	}
	var genesis evmtypes.GenesisState
	if err := cod.UnmarshalJSON(raw, &genesis); err != nil {
		return err
	}
	return validateVMGenesisPolicy(genesis, appModule.installed)
}

func (appModule guardedVMAppModule) InitGenesis(
	ctx sdk.Context,
	cod codec.JSONCodec,
	raw json.RawMessage,
) []abci.ValidatorUpdate {
	var genesis evmtypes.GenesisState
	cod.MustUnmarshalJSON(raw, &genesis)
	if err := genesis.Validate(); err != nil {
		panic(fmt.Errorf("invalid VM genesis: %w", err))
	}
	if err := validateVMGenesisPolicy(genesis, appModule.installed); err != nil {
		panic(fmt.Errorf("invalid VM genesis: %w", err))
	}
	return appModule.AppModule.InitGenesis(ctx, cod, raw)
}
