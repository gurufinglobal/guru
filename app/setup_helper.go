package app

import (
	"fmt"
	goruntime "runtime"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/baseapp/txnrunner"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/evm/x/erc20"
	vmrunner "github.com/cosmos/evm/x/vm/runner"
	ibccallbacks "github.com/cosmos/ibc-go/v11/modules/apps/callbacks"
	transfer "github.com/cosmos/ibc-go/v11/modules/apps/transfer"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	transwap "github.com/gurufinglobal/guru/v3/x/ibc/transwap"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
	"github.com/spf13/cast"
)

func (app *App) mountStoresAndSetABCIHandlers() {
	app.MountKVStores(app.GetKVStoreKeys())
	app.MountTransientStores(app.GetTransientStoreKeys())
	app.MountObjectStores(app.GetObjectStoreKeys())

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
}

func (app *App) configureIBCRouters() ibctm.LightClientModule {
	var transferStack porttypes.IBCModule

	transferStack = transfer.NewIBCModule(app.TransferKeeper)
	maxCallbackGas := uint64(1_000_000)
	transferStack = erc20.NewIBCMiddleware(app.Erc20Keeper, transferStack)
	callbacksMiddleware := ibccallbacks.NewIBCMiddleware(app.CallbackKeeper, maxCallbackGas)
	callbacksMiddleware.SetICS4Wrapper(app.IBCKeeper.ChannelKeeper)
	callbacksMiddleware.SetUnderlyingApplication(transferStack)
	transferStack = callbacksMiddleware

	transwapStack := transwap.NewIBCModule(app.TranswapKeeper)

	ibcRouter := porttypes.NewRouter()
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack)
	ibcRouter.AddRoute(transwaptypes.ModuleName, transwapStack)

	app.IBCKeeper.SetRouter(ibcRouter)

	clientKeeper := app.IBCKeeper.ClientKeeper
	storeProvider := app.IBCKeeper.ClientKeeper.GetStoreProvider()
	tmLightClientModule := ibctm.NewLightClientModule(app.appCodec, storeProvider)
	clientKeeper.AddRoute(ibctm.ModuleName, &tmLightClientModule)

	return tmLightClientModule
}

func (app *App) configureModuleManager(tmLightClientModule ibctm.LightClientModule) wiredAppModules {
	modules := appModules(app, app.appCodec, app.txConfig, tmLightClientModule)
	app.ModuleManager = module.NewManagerFromMap(
		moduleManagerMap(modules.modules),
	)
	app.BasicModuleManager = newBasicManagerFromManager(app)

	app.ModuleManager.SetOrderPreBlockers(ModuleOrderPreBlockers()...)
	app.ModuleManager.SetOrderBeginBlockers(ModuleOrderBeginBlockers()...)
	app.ModuleManager.SetOrderEndBlockers(ModuleOrderEndBlockers()...)
	genesisModuleOrder := ModuleOrderInitGenesis()
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)

	// Keep service registration after module manager and configurator setup so
	// modules can attach Msg and Query services to the final routers.
	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(fmt.Sprintf("failed to register services in module manager: %s", err.Error()))
	}

	app.RegisterUpgradeHandlers()

	autocliv1.RegisterQueryServer(app.GRPCQueryRouter(), runtimeservices.NewAutoCLIQueryService(app.ModuleManager.Modules))

	reflectionSvc, err := runtimeservices.NewReflectionService()
	if err != nil {
		panic(err)
	}
	reflectionv1.RegisterReflectionServiceServer(app.GRPCQueryRouter(), reflectionSvc)

	return modules
}

func (app *App) configureOracleVoteExtensions(appOpts servertypes.AppOptions) {
	oracleEnabled := true
	if value := appOpts.Get("oracle.enabled"); value != nil {
		oracleEnabled = cast.ToBool(value)
	}

	oracleVoteHandler := oracleabci.NewVoteExtensionHandler(
		app.OracleKeeper,
		oracleEnabled,
		cast.ToString(appOpts.Get("oracle.sidecar_socket")),
		cast.ToDuration(appOpts.Get("oracle.sidecar_timeout")),
	)
	app.SetExtendVoteHandler(oracleVoteHandler.ExtendVote)
	app.SetVerifyVoteExtensionHandler(oracleVoteHandler.VerifyVoteExtension)
}

func (app *App) configureVMRunner(
	bApp *baseapp.BaseApp,
	txDecoder sdk.TxDecoder,
	nonTransientKeys []storetypes.StoreKey,
) {
	vmrunner.SetRunner(bApp, oracleabci.NewPayloadSkippingTxRunner(txnrunner.NewSTMRunner(
		txDecoder,
		nonTransientKeys,
		min(goruntime.GOMAXPROCS(0), goruntime.NumCPU()),
		true,
		func(ms storetypes.MultiStore) string {
			denom := app.EVMKeeper.GetParams(
				sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger()),
			).EvmDenom
			if denom == "" {
				return appparams.BaseDenom
			}
			return denom
		},
	)))
}
