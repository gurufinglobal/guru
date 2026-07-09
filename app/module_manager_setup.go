package app

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	"github.com/cosmos/cosmos-sdk/types/module"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
)

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
