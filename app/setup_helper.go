package app

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/evm/x/erc20"
	ibccallbacks "github.com/cosmos/ibc-go/v10/modules/apps/callbacks"
	transfer "github.com/cosmos/ibc-go/v10/modules/apps/transfer"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	porttypes "github.com/cosmos/ibc-go/v10/modules/core/05-port/types"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"
	transwap "github.com/gurufinglobal/guru/v3/x/ibc/transwap"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
	"github.com/spf13/cast"
)

func (app *App) mountStoresAndSetABCIHandlers() {
	app.MountKVStores(app.GetKVStoreKeys())
	app.MountTransientStores(app.GetTransientStoreKeys())

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
	transferStack = ibccallbacks.NewIBCMiddleware(
		transferStack,
		app.IBCKeeper.ChannelKeeper,
		app.CallbackKeeper,
		maxCallbackGas,
	)
	transferICS4Wrapper, ok := transferStack.(porttypes.ICS4Wrapper)
	if !ok {
		panic(fmt.Errorf("transfer stack %T does not implement ICS4Wrapper", transferStack))
	}
	app.TransferKeeper.WithICS4Wrapper(transferICS4Wrapper)

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

	oracleVoteHandler, err := oracleabci.NewVoteExtensionHandler(
		app.OracleKeeper,
		oracleEnabled,
		cast.ToString(appOpts.Get("oracle.sidecar_socket")),
		cast.ToDuration(appOpts.Get("oracle.sidecar_timeout")),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to configure oracle vote extension handler: %s", err))
	}
	app.oracleVoteHandler = oracleVoteHandler
	app.SetExtendVoteHandler(oracleVoteHandler.ExtendVote)
	app.SetVerifyVoteExtensionHandler(oracleVoteHandler.VerifyVoteExtension)
}
