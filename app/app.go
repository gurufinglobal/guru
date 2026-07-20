package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"
	"github.com/ethereum/go-ethereum/common"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth/posthandler"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	evmante "github.com/cosmos/evm/ante"
	evmaddress "github.com/cosmos/evm/encoding/address"
	cosmosevmserver "github.com/cosmos/evm/server"
	srvflags "github.com/cosmos/evm/server/flags"
	evmutils "github.com/cosmos/evm/utils"
	gogoproto "github.com/cosmos/gogoproto/proto"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"
	appkeepers "github.com/gurufinglobal/guru/v3/app/keepers"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
	"github.com/spf13/cast"
)

func init() {
	// manually update the power reduction by replacing micro (u) -> atto (a) evmos
	sdk.DefaultPowerReduction = evmutils.AttoPowerReduction
}

var (
	_ runtime.AppI                = (*App)(nil)
	_ cosmosevmserver.Application = (*App)(nil)
	// _ ibctesting.TestingApp       = (*App)(nil)
)

type App struct {
	*baseapp.BaseApp

	appCodec          codec.Codec
	interfaceRegistry codectypes.InterfaceRegistry
	txConfig          client.TxConfig

	pendingTxListeners []evmante.PendingTxListener

	anteHandler sdk.AnteHandler

	EVMMempool            sdkmempool.ExtMempool
	OracleProposalHandler *oracleabci.ProposalHandler

	*appkeepers.AppKeepers

	// the module manager
	ModuleManager      *module.Manager
	BasicModuleManager module.BasicManager

	// simulation manager
	sm *module.SimulationManager

	// module configurator
	configurator module.Configurator
}

func NewApp(
	logger log.Logger,
	db dbm.DB,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	evmChainID := cast.ToUint64(appOpts.Get(srvflags.EVMChainID))
	encodingConfig := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)

	baseAppOptions = append(
		baseAppOptions,
		baseapp.SetOptimisticExecution(),
	)

	bApp := baseapp.NewBaseApp(
		appparams.AppName,
		logger,
		db,
		// use transaction decoder to support the sdk.Tx interface instead of sdk.StdTx
		encodingConfig.TxConfig.TxDecoder(),
		baseAppOptions...,
	)

	// bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(encodingConfig.InterfaceRegistry)
	bApp.SetTxEncoder(encodingConfig.TxConfig.TxEncoder())

	skipUpgradeHeights := map[int64]bool{}
	for _, h := range cast.ToIntSlice(appOpts.Get(sdkserver.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}
	homePath := cast.ToString(appOpts.Get(flags.FlagHome))
	evmTracer := cast.ToString(appOpts.Get(srvflags.EVMTracer))

	appKeepers := appkeepers.NewAppKeepers(appparams.KeepersInitConfig{
		AppCodec:               encodingConfig.Codec,
		BaseApp:                bApp,
		Logger:                 logger,
		HomePath:               homePath,
		SkipUpgradeHeights:     skipUpgradeHeights,
		AccountAddressPrefix:   appparams.Bech32PrefixAccAddr,
		ValidatorAddressPrefix: appparams.Bech32PrefixValAddr,
		ConsensusAddressPrefix: appparams.Bech32PrefixConsAddr,
		EVMChainID:             evmChainID,
		EVMTracer:              evmTracer,
		ModuleAccountPerms:     appparams.DefaultModuleAccountPermissions(),
	})

	// disable block gas meter
	bApp.SetDisableBlockGasMeter(true)

	// load state streaming if enabled
	if err := bApp.RegisterStreamingServices(appOpts, appKeepers.GetKVStoreKeys()); err != nil {
		panic(fmt.Sprintf("failed to load state streaming: %s", err))
	}

	// wire up the versiondb's `StreamingService` and `MultiStore`.
	if cast.ToBool(appOpts.Get("versiondb.enable")) {
		panic(fmt.Sprintf("version db not supported in this %s chain", appparams.AppName))
	}

	app := &App{
		BaseApp:           bApp,
		appCodec:          encodingConfig.Codec,
		txConfig:          encodingConfig.TxConfig,
		interfaceRegistry: encodingConfig.InterfaceRegistry,
		AppKeepers:        appKeepers,
	}
	app.configureOracleVoteExtensions(appOpts)
	tmLightClientModule := app.configureIBCRouters()
	modules := app.configureModuleManager(tmLightClientModule)
	app.mountStoresAndSetABCIHandlers()

	maxGasWanted := cast.ToUint64(appOpts.Get(srvflags.EVMMaxTxGasWanted))
	if err := app.setAnteHandler(app.txConfig, maxGasWanted); err != nil {
		panic(fmt.Sprintf("failed to configure ante handler: %s", err.Error()))
	}
	if err := app.configureEVMMempool(appOpts, logger); err != nil {
		panic(fmt.Sprintf("failed to configure EVM mempool: %s", err.Error()))
	}
	app.setPostHandler()

	// Validate protobuf annotations after all modules are registered.
	if err := msgservice.ValidateProtoAnnotations(gogoproto.HybridResolver); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}

	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			logger.Error("error on loading last version", "err", err)
			os.Exit(1)
		}

		// Ensure EVM globals are hydrated before servicing RPC/query traffic after restart.
		modules.evm.HydrateGlobals(app.NewContextLegacy(true, cmtproto.Header{
			Height:  app.LastBlockHeight(),
			ChainID: app.ChainID(),
		}))
	}

	app.configureVMRunner(bApp, encodingConfig.TxConfig.TxDecoder(), appKeepers.GetNonTransientKeys())

	return app
}

// Name returns the name of the App
func (app *App) Name() string {
	return app.BaseApp.Name()
}

// BeginBlocker application updates every begin block
func (app *App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

// EndBlocker application updates every end block
func (app *App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.ModuleManager.EndBlock(ctx)
}

func (app *App) FinalizeBlock(req *abci.RequestFinalizeBlock) (res *abci.ResponseFinalizeBlock, err error) {
	return app.BaseApp.FinalizeBlock(req)
}

func (app *App) Configurator() module.Configurator {
	return app.configurator
}

// InitChainer application update at chain initialization
func (app *App) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	var genesisState map[string]json.RawMessage
	// var genesisState GenesisState
	if err := json.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		panic(err)
	}
	if err := app.ValidateChainGenesis(genesisState); err != nil {
		return nil, fmt.Errorf("invalid chain genesis: %w", err)
	}

	if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
		panic(err)
	}

	return app.ModuleManager.InitGenesis(ctx, app.appCodec, genesisState)
}

func (app *App) PreBlocker(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	if app.OracleProposalHandler != nil {
		if err := app.OracleProposalHandler.ApplyProposalPayload(ctx, req); err != nil {
			return nil, err
		}
	}
	return app.ModuleManager.PreBlock(ctx)
}

// LoadHeight loads a particular height
func (app *App) LoadHeight(height int64) error {
	return app.LoadVersion(height)
}

func (app *App) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	// Register new tx routes from grpc-gateway.
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// Register new cometbft queries routes from grpc-gateway.
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// Register node gRPC service for grpc-gateway.
	node.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// Register grpc-gateway routes for all modules.
	app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// register swagger API from root so that other applications can override easily
	if err := sdkserver.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

// RegisterTxService implements the Application.RegisterTxService method.
func (app *App) RegisterTxService(clientCtx client.Context) {
	registerTxServiceNoAmino(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.interfaceRegistry)
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	cmtservice.RegisterTendermintService(
		clientCtx,
		app.GRPCQueryRouter(),
		app.interfaceRegistry,
		app.Query,
	)
}

func (app *App) RegisterNodeService(clientCtx client.Context, cfg config.Config) {
	node.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg, func() int64 {
		return app.CommitMultiStore().EarliestVersion()
	})
}

// Close unsubscribes from the CometBFT event bus (if set) and closes the mempool and underlying BaseApp.
func (app *App) Close() error {
	var err error
	msg := "Application gracefully shutdown"
	if m, ok := app.EVMMempool.(io.Closer); ok && m != nil {
		err = errors.Join(err, m.Close())
	}
	err = errors.Join(err, app.BaseApp.Close())
	if err == nil {
		app.Logger().Info(msg)
	} else {
		app.Logger().Error(msg, "error", err)
	}

	return err
}

func (app *App) AutoCliOpts() autocli.AppOptions {
	modules := make(map[string]appmodule.AppModule, len(app.ModuleManager.Modules))
	for moduleName, m := range app.ModuleManager.Modules {
		if appModule, ok := m.(appmodule.AppModule); ok {
			modules[moduleName] = appModule
		}
	}

	return autocli.AppOptions{
		Modules:       modules,
		ModuleOptions: runtimeservices.ExtractAutoCLIOptions(app.ModuleManager.Modules),
		// Keep CLI address codecs explicit instead of relying on global SDK config.
		AddressCodec:          evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr),
		ValidatorAddressCodec: evmaddress.NewEvmCodec(appparams.Bech32PrefixValAddr),
		ConsensusAddressCodec: evmaddress.NewEvmCodec(appparams.Bech32PrefixConsAddr),
	}
}

// RegisterPendingTxListener allows JSON-RPC server to subscribe to pending tx callbacks.
func (app *App) RegisterPendingTxListener(listener func(common.Hash)) {
	app.pendingTxListeners = append(app.pendingTxListeners, listener)
}

func (app *App) GetMempool() sdkmempool.ExtMempool {
	return app.EVMMempool
}

func (app *App) setPostHandler() {
	postHandler, err := posthandler.NewPostHandler(
		posthandler.HandlerOptions{},
	)
	if err != nil {
		panic(err)
	}

	app.SetPostHandler(postHandler)
}

func (app *App) LegacyAmino() *codec.LegacyAmino {
	return nil
}

func (app *App) AppCodec() codec.Codec {
	return app.appCodec
}

func (app *App) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

func (app *App) TxConfig() client.TxConfig {
	return app.txConfig
}

func (app *App) SimulationManager() *module.SimulationManager {
	return app.sm
}
