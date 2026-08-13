package app

import (
	"fmt"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/x/evidence"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantmodule "cosmossdk.io/x/feegrant/module"
	"cosmossdk.io/x/upgrade"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authsimulation "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/mint"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/evm/x/erc20"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	erc20v2 "github.com/cosmos/evm/x/erc20/v2"
	"github.com/cosmos/evm/x/feemarket"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/cosmos/evm/x/vm"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	transfer "github.com/cosmos/ibc-go/v10/modules/apps/transfer"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	transferv2 "github.com/cosmos/ibc-go/v10/modules/apps/transfer/v2"
	ibc "github.com/cosmos/ibc-go/v10/modules/core"
	porttypes "github.com/cosmos/ibc-go/v10/modules/core/05-port/types"
	ibcapi "github.com/cosmos/ibc-go/v10/modules/core/api"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"

	constitution "github.com/gurufinglobal/guru/v2/x/constitution"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
)

var (
	preBlockerOrder = []string{
		upgradetypes.ModuleName,
		authtypes.ModuleName,
		evmtypes.ModuleName,
	}
	beginBlockerOrder = []string{
		minttypes.ModuleName,
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
		erc20types.ModuleName,
		feemarkettypes.ModuleName,
		evmtypes.ModuleName,
		constitutiontypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		evidencetypes.ModuleName,
		stakingtypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		govtypes.ModuleName,
		genutiltypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		consensusparamtypes.ModuleName,
		vestingtypes.ModuleName,
	}
	endBlockerOrder = []string{
		govtypes.ModuleName,
		stakingtypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		evmtypes.ModuleName,
		erc20types.ModuleName,
		feemarkettypes.ModuleName,
		constitutiontypes.ModuleName,
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		minttypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		upgradetypes.ModuleName,
		consensusparamtypes.ModuleName,
		vestingtypes.ModuleName,
	}
	initGenesisOrder = []string{
		authtypes.ModuleName,
		banktypes.ModuleName,
		constitutiontypes.ModuleName,
		distrtypes.ModuleName,
		stakingtypes.ModuleName,
		slashingtypes.ModuleName,
		govtypes.ModuleName,
		minttypes.ModuleName,
		ibcexported.ModuleName,
		evmtypes.ModuleName,
		feemarkettypes.ModuleName,
		erc20types.ModuleName,
		ibctransfertypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		upgradetypes.ModuleName,
		vestingtypes.ModuleName,
	}
)

func (app *App) configureIBC() ibctm.LightClientModule {
	var transferStack porttypes.IBCModule = transfer.NewIBCModule(app.TransferKeeper)
	transferStack = erc20.NewIBCMiddleware(app.ERC20Keeper, transferStack)
	ibcRouter := porttypes.NewRouter()
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack)

	var transferStackV2 ibcapi.IBCModule = transferv2.NewIBCModule(app.TransferKeeper)
	transferStackV2 = erc20v2.NewIBCMiddleware(transferStackV2, app.ERC20Keeper)
	ibcRouterV2 := ibcapi.NewRouter()
	ibcRouterV2.AddRoute(ibctransfertypes.ModuleName, transferStackV2)

	app.IBCKeeper.SetRouter(ibcRouter)
	app.IBCKeeper.SetRouterV2(ibcRouterV2)

	storeProvider := app.IBCKeeper.ClientKeeper.GetStoreProvider()
	tmLightClient := ibctm.NewLightClientModule(app.AppCodec(), storeProvider)
	app.IBCKeeper.ClientKeeper.AddRoute(ibctm.ModuleName, &tmLightClient)
	return tmLightClient
}

func (app *App) configureModules(tmLightClient ibctm.LightClientModule) error {
	vmModule := vm.NewAppModule(
		app.EVMKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		app.AccountKeeper.AddressCodec(),
	)

	modules := []appmodule.AppModule{
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, app.GetTxConfig()),
		auth.NewAppModule(app.AppCodec(), app.AccountKeeper, authsimulation.RandomGenesisAccounts, nil),
		bank.NewAppModule(app.AppCodec(), app.BankKeeper, app.AccountKeeper, nil),
		constitution.NewAppModule(app.ConstitutionKeeper),
		feegrantmodule.NewAppModule(app.AppCodec(), app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, app.InterfaceRegistry()),
		gov.NewAppModule(app.AppCodec(), &app.GovKeeper, app.AccountKeeper, app.BankKeeper, nil),
		mint.NewAppModule(app.AppCodec(), app.MintKeeper, app.AccountKeeper, nil, nil),
		slashing.NewAppModule(app.AppCodec(), app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil, app.InterfaceRegistry()),
		distribution.NewAppModule(app.AppCodec(), app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil),
		staking.NewAppModule(app.AppCodec(), app.StakingKeeper, app.AccountKeeper, app.BankKeeper, nil),
		upgrade.NewAppModule(app.UpgradeKeeper, app.AccountKeeper.AddressCodec()),
		evidence.NewAppModule(app.EvidenceKeeper),
		authzmodule.NewAppModule(app.AppCodec(), app.AuthzKeeper, app.AccountKeeper, app.BankKeeper, app.InterfaceRegistry()),
		consensus.NewAppModule(app.AppCodec(), app.ConsensusParamsKeeper),
		vesting.NewAppModule(app.AccountKeeper, app.BankKeeper),
		ibc.NewAppModule(app.IBCKeeper),
		ibctm.NewAppModule(tmLightClient),
		transfer.NewAppModule(app.TransferKeeper),
		vmModule,
		feemarket.NewAppModule(app.FeeMarketKeeper),
		erc20.NewAppModule(app.ERC20Keeper, app.AccountKeeper),
	}

	moduleMap, err := namedModuleMap(modules)
	if err != nil {
		return err
	}
	app.ModuleManager = module.NewManagerFromMap(moduleMap)
	app.BasicModuleManager = module.NewBasicManagerFromManager(app.ModuleManager, NewBasicManager())

	app.ModuleManager.SetOrderPreBlockers(preBlockerOrder...)
	app.ModuleManager.SetOrderBeginBlockers(beginBlockerOrder...)
	app.ModuleManager.SetOrderEndBlockers(endBlockerOrder...)
	app.ModuleManager.SetOrderInitGenesis(initGenesisOrder...)
	app.ModuleManager.SetOrderExportGenesis(initGenesisOrder...)

	app.configurator = module.NewConfigurator(app.AppCodec(), app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		return fmt.Errorf("register module services: %w", err)
	}
	if err := constitution.RegisterMigrations(app.configurator); err != nil {
		return fmt.Errorf("register constitution migrations: %w", err)
	}
	return nil
}

// NewBasicManager builds the stateless module surface used by both the
// application and the CLI. Keeping this independent from App construction is
// important: constructing a temporary EVM keeper in the root command would
// consume Cosmos EVM's process-wide chain configuration before `start` creates
// the real application.
func NewBasicManager() module.BasicManager {
	return module.NewBasicManager(
		genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
		auth.AppModuleBasic{},
		bank.AppModuleBasic{},
		feegrantmodule.AppModuleBasic{},
		gov.NewAppModuleBasic(nil),
		mint.AppModuleBasic{},
		slashing.AppModuleBasic{},
		distribution.AppModuleBasic{},
		staking.AppModuleBasic{},
		upgrade.AppModuleBasic{},
		evidence.NewAppModuleBasic(),
		authzmodule.AppModuleBasic{},
		consensus.AppModuleBasic{},
		vesting.AppModuleBasic{},
		ibc.AppModuleBasic{},
		ibctm.AppModuleBasic{},
		transfer.AppModuleBasic{},
		vm.AppModuleBasic{},
		feemarket.AppModuleBasic{},
		erc20.AppModuleBasic{},
	)
}

func namedModuleMap(modules []appmodule.AppModule) (map[string]appmodule.AppModule, error) {
	moduleMap := make(map[string]appmodule.AppModule, len(modules))
	for _, appModule := range modules {
		named, ok := appModule.(module.HasName)
		if !ok {
			return nil, fmt.Errorf("app module %T has no name", appModule)
		}
		name := named.Name()
		if _, exists := moduleMap[name]; exists {
			return nil, fmt.Errorf("duplicate app module name %q", name)
		}
		moduleMap[name] = appModule
	}
	return moduleMap, nil
}
