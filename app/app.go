package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	goruntime "runtime"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"
	"github.com/ethereum/go-ethereum/common"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/baseapp/txnrunner"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth/posthandler"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	evmante "github.com/cosmos/evm/ante"
	evmaddress "github.com/cosmos/evm/encoding/address"
	evmutils "github.com/cosmos/evm/utils"
	"github.com/cosmos/evm/x/erc20"
	erc20v2 "github.com/cosmos/evm/x/erc20/v2"
	"github.com/cosmos/gogoproto/proto"
	ibccallbacks "github.com/cosmos/ibc-go/v11/modules/apps/callbacks"
	transfer "github.com/cosmos/ibc-go/v11/modules/apps/transfer"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	transferv2 "github.com/cosmos/ibc-go/v11/modules/apps/transfer/v2"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcapi "github.com/cosmos/ibc-go/v11/modules/core/api"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	appkeepers "github.com/gurufinglobal/guru/v3/app/keepers"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/spf13/cast"

	srvflags "github.com/cosmos/evm/server/flags"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"
)

func init() {
	// manually update the power reduction by replacing micro (u) -> atto (a) evmos
	sdk.DefaultPowerReduction = evmutils.AttoPowerReduction
}

type App struct {
	*baseapp.BaseApp

	appCodec          codec.Codec
	interfaceRegistry codectypes.InterfaceRegistry
	txConfig          client.TxConfig
	clientCtx         client.Context

	pendingTxListeners []evmante.PendingTxListener

	anteHandler sdk.AnteHandler

	EVMMempool sdkmempool.ExtMempool

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
	traceStore io.Writer,
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

	bApp.SetBlockSTMTxRunner(txnrunner.NewSTMRunner(
		encodingConfig.TxConfig.TxDecoder(),
		appKeepers.GetNonTransientKeys(),
		min(goruntime.GOMAXPROCS(0), goruntime.NumCPU()),
		true,
		func(ms storetypes.MultiStore) string { return appparams.BaseDenom },
	))

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

	var transferStack porttypes.IBCModule

	transferStack = transfer.NewIBCModule(app.TransferKeeper)
	maxCallbackGas := uint64(1_000_000)
	transferStack = erc20.NewIBCMiddleware(app.Erc20Keeper, transferStack)
	callbacksMiddleware := ibccallbacks.NewIBCMiddleware(app.CallbackKeeper, maxCallbackGas)
	callbacksMiddleware.SetICS4Wrapper(app.IBCKeeper.ChannelKeeper)
	callbacksMiddleware.SetUnderlyingApplication(transferStack)
	transferStack = callbacksMiddleware

	var transferStackV2 ibcapi.IBCModule
	transferStackV2 = transferv2.NewIBCModule(app.TransferKeeper)
	transferStackV2 = erc20v2.NewIBCMiddleware(transferStackV2, app.Erc20Keeper)

	// Create static IBC router, add transfer route, then set and seal it
	ibcRouter := porttypes.NewRouter()
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack)
	ibcRouterV2 := ibcapi.NewRouter()
	ibcRouterV2.AddRoute(ibctransfertypes.ModuleName, transferStackV2)

	app.IBCKeeper.SetRouter(ibcRouter)
	app.IBCKeeper.SetRouterV2(ibcRouterV2)

	clientKeeper := app.IBCKeeper.ClientKeeper
	storeProvider := app.IBCKeeper.ClientKeeper.GetStoreProvider()
	tmLightClientModule := ibctm.NewLightClientModule(app.appCodec, storeProvider)
	clientKeeper.AddRoute(ibctm.ModuleName, &tmLightClientModule)

	app.ModuleManager = module.NewManager(
		moduleManagerModules(appModules(app, app.appCodec, app.txConfig, tmLightClientModule))...,
	)
	app.BasicModuleManager = newBasicManagerFromManager(app)

	app.ModuleManager.SetOrderPreBlockers(ModuleOrderPreBlockers()...)
	app.ModuleManager.SetOrderBeginBlockers(ModuleOrderBeginBlockers()...)
	app.ModuleManager.SetOrderEndBlockers(ModuleOrderEndBlockers()...)
	genesisModuleOrder := ModuleOrderInitGenesis()
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)

	// Uncomment if you want to set a custom migration order here.
	// app.ModuleManager.SetOrderMigrations(custom order)

	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(fmt.Sprintf("failed to register services in module manager: %s", err.Error()))
	}

	// RegisterUpgradeHandlers is used for registering any on-chain upgrades.
	// Make sure it's called after `app.ModuleManager` and `app.configurator` are set.
	app.RegisterUpgradeHandlers()

	autocliv1.RegisterQueryServer(app.GRPCQueryRouter(), runtimeservices.NewAutoCLIQueryService(app.ModuleManager.Modules))

	reflectionSvc, err := runtimeservices.NewReflectionService()
	if err != nil {
		panic(err)
	}
	reflectionv1.RegisterReflectionServiceServer(app.GRPCQueryRouter(), reflectionSvc)

	// 1. 스토어 마운트
	app.MountKVStores(app.GetKVStoreKeys())
	app.MountObjectStores(app.GetObjectStoreKeys())

	// 2. 가스 제한 및 ABCI 라이프사이클 연결
	maxGasWanted := cast.ToUint64(appOpts.Get(srvflags.EVMMaxTxGasWanted))
	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)

	// 3. 트랜잭션 문지기 및 EVM 멤풀 연결
	if err := app.setAnteHandler(app.txConfig, maxGasWanted); err != nil {
		panic(fmt.Sprintf("failed to configure ante handler: %s", err.Error()))
	}
	if err := app.configureEVMMempool(appOpts, logger); err != nil {
		panic(fmt.Sprintf("failed to configure EVM mempool: %s", err.Error()))
	}
	app.setPostHandler()

	// 4. Protobuf 검증 (선택적이지만 권장)
	protoFiles, err := proto.MergedRegistry()
	if err != nil {
		panic(err)
	}
	if err := msgservice.ValidateProtoAnnotations(protoFiles); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}

	// 5. 체인 부팅 완료
	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			logger.Error("error on loading last version", "err", err)
			os.Exit(1)
		}
	}

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

	if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
		panic(err)
	}

	return app.ModuleManager.InitGenesis(ctx, app.appCodec, genesisState)
}

func (app *App) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
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
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.interfaceRegistry)
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
	modules := make(map[string]appmodule.AppModule, 0)
	for _, m := range app.ModuleManager.Modules {
		if moduleWithName, ok := m.(module.HasName); ok {
			moduleName := moduleWithName.Name()
			if appModule, ok := moduleWithName.(appmodule.AppModule); ok {
				modules[moduleName] = appModule
			}
		}
	}

	return autocli.AppOptions{
		Modules:       modules,
		ModuleOptions: runtimeservices.ExtractAutoCLIOptions(app.ModuleManager.Modules),
		// sdk.GetConfig()를 완전히 제거하고 Guru 상수를 직접 주입합니다.
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
