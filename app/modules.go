package app

import (
	"fmt"
	"sync"

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
	"github.com/cosmos/evm/x/feemarket"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/cosmos/evm/x/vm"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibc "github.com/cosmos/ibc-go/v10/modules/core"
	porttypes "github.com/cosmos/ibc-go/v10/modules/core/05-port/types"
	ibcapi "github.com/cosmos/ibc-go/v10/modules/core/api"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"
)

var registerModuleCodecsOnce sync.Once

var (
	preBlockerOrder = []string{
		upgradetypes.ModuleName,
		authtypes.ModuleName,
		evmtypes.ModuleName,
	}
	beginBlockerOrder = []string{
		minttypes.ModuleName,
		ibcexported.ModuleName,
		feemarkettypes.ModuleName,
		evmtypes.ModuleName,
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
		feemarkettypes.ModuleName,
		ibcexported.ModuleName,
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
		distrtypes.ModuleName,
		stakingtypes.ModuleName,
		slashingtypes.ModuleName,
		govtypes.ModuleName,
		minttypes.ModuleName,
		ibcexported.ModuleName,
		evmtypes.ModuleName,
		feemarkettypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		upgradetypes.ModuleName,
		vestingtypes.ModuleName,
	}
)

func (app *App) configureIBCCore() ibctm.LightClientModule {
	app.IBCKeeper.SetRouter(porttypes.NewRouter())
	app.IBCKeeper.SetRouterV2(ibcapi.NewRouter())

	storeProvider := app.IBCKeeper.ClientKeeper.GetStoreProvider()
	tmLightClient := ibctm.NewLightClientModule(app.AppCodec(), storeProvider)
	app.IBCKeeper.ClientKeeper.AddRoute(ibctm.ModuleName, &tmLightClient)
	return tmLightClient
}

func (app *App) configureModules(tmLightClient ibctm.LightClientModule) error {
	upstreamVMModule := vm.NewAppModule(
		app.EVMKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		app.AccountKeeper.AddressCodec(),
	)
	vmModule := newGuardedVMAppModule(upstreamVMModule, app.installedPrecompiles)

	modules := []appmodule.AppModule{
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, app.TxConfig()),
		auth.NewAppModule(app.AppCodec(), app.AccountKeeper, authsimulation.RandomGenesisAccounts, nil),
		bank.NewAppModule(app.AppCodec(), app.BankKeeper, app.AccountKeeper, nil),
		feegrantmodule.NewAppModule(app.AppCodec(), app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, app.InterfaceRegistry()),
		newGuardedGovAppModule(gov.NewAppModule(app.AppCodec(), &app.GovKeeper, app.AccountKeeper, app.BankKeeper, nil)),
		newGuardedMintAppModule(mint.NewAppModule(app.AppCodec(), app.MintKeeper, app.AccountKeeper, nil, nil)),
		slashing.NewAppModule(app.AppCodec(), app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil, app.InterfaceRegistry()),
		distribution.NewAppModule(app.AppCodec(), app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil),
		newGuardedStakingAppModule(staking.NewAppModule(app.AppCodec(), app.StakingKeeper, app.AccountKeeper, app.BankKeeper, nil)),
		upgrade.NewAppModule(app.UpgradeKeeper, app.AccountKeeper.AddressCodec()),
		evidence.NewAppModule(app.EvidenceKeeper),
		authzmodule.NewAppModule(app.AppCodec(), app.AuthzKeeper, app.AccountKeeper, app.BankKeeper, app.InterfaceRegistry()),
		consensus.NewAppModule(app.AppCodec(), app.ConsensusParamsKeeper),
		vesting.NewAppModule(app.AccountKeeper, app.BankKeeper),
		ibc.NewAppModule(app.IBCKeeper),
		ibctm.NewAppModule(tmLightClient),
		vmModule,
		newGuardedFeeMarketAppModule(
			feemarket.NewAppModule(app.FeeMarketKeeper),
			app.FeeMarketKeeper,
		),
	}

	moduleMap, err := namedModuleMap(modules)
	if err != nil {
		return err
	}
	app.ModuleManager = module.NewManagerFromMap(moduleMap)
	app.BasicModuleManager = module.NewBasicManagerFromManager(
		app.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			stakingtypes.ModuleName: staking.AppModuleBasic{},
			govtypes.ModuleName:     gov.NewAppModuleBasic(nil),
		},
	)
	registerModuleCodecsOnce.Do(func() {
		app.BasicModuleManager.RegisterLegacyAminoCodec(app.LegacyAmino())
		app.BasicModuleManager.RegisterInterfaces(app.InterfaceRegistry())
	})

	app.ModuleManager.SetOrderPreBlockers(preBlockerOrder...)
	app.ModuleManager.SetOrderBeginBlockers(beginBlockerOrder...)
	app.ModuleManager.SetOrderEndBlockers(endBlockerOrder...)
	app.ModuleManager.SetOrderInitGenesis(initGenesisOrder...)
	app.ModuleManager.SetOrderExportGenesis(initGenesisOrder...)

	app.configurator = module.NewConfigurator(app.AppCodec(), app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		return fmt.Errorf("register module services: %w", err)
	}
	return nil
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

func copyModuleOrder(order []string) []string {
	return append([]string(nil), order...)
}

// ModuleOrderPreBlockers returns a defensive copy of the consensus-critical order.
func ModuleOrderPreBlockers() []string { return copyModuleOrder(preBlockerOrder) }

// ModuleOrderBeginBlockers returns a defensive copy of the consensus-critical order.
func ModuleOrderBeginBlockers() []string { return copyModuleOrder(beginBlockerOrder) }

// ModuleOrderEndBlockers returns a defensive copy of the consensus-critical order.
func ModuleOrderEndBlockers() []string { return copyModuleOrder(endBlockerOrder) }

// ModuleOrderInitGenesis returns a defensive copy of the consensus-critical order.
func ModuleOrderInitGenesis() []string { return copyModuleOrder(initGenesisOrder) }
