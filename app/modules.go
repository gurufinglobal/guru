package app

import (
	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/evidence"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	feegrantmodule "github.com/cosmos/cosmos-sdk/x/feegrant/module"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/mint"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	sdkstaking "github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/cosmos/evm/x/erc20"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	"github.com/cosmos/evm/x/feemarket"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/cosmos/evm/x/vm"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	transfer "github.com/cosmos/ibc-go/v11/modules/apps/transfer"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibc "github.com/cosmos/ibc-go/v11/modules/core"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	constitution "github.com/gurufinglobal/guru/v3/x/constitution"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	customstaking "github.com/gurufinglobal/guru/v3/x/staking"
)

var (
	preBlockerOrder = []string{
		upgradetypes.ModuleName,
		authtypes.ModuleName,
		evmtypes.ModuleName,
	}

	beginBlockerOrder = []string{
		minttypes.ModuleName,

		// IBC modules
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,

		// Cosmos EVM BeginBlockers
		erc20types.ModuleName,
		feemarkettypes.ModuleName,
		evmtypes.ModuleName, // NOTE: EVM BeginBlocker must come after FeeMarket BeginBlocker

		// constitution separation must run before distribution.
		constitutiontypes.ModuleName,

		// no-op and legacy blockers
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
		banktypes.ModuleName,
		govtypes.ModuleName,
		stakingtypes.ModuleName,
		authtypes.ModuleName,

		// Cosmos EVM EndBlockers
		evmtypes.ModuleName,
		erc20types.ModuleName,
		feemarkettypes.ModuleName,

		// no-op and legacy blockers
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

		// Cosmos EVM modules
		// NOTE: feemarket module needs to be initialized before genutil module:
		// gentx transactions use MinGasPriceDecorator.AnteHandle
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

func appModules(
	app *App,
	appCodec codec.Codec,
	txConfig client.TxConfig,
	tmLightClientModule ibctm.LightClientModule,
) wiredAppModules {
	vmModule := vm.NewAppModule(app.EVMKeeper, app.AccountKeeper, app.BankKeeper, app.AccountKeeper.AddressCodec())

	return wiredAppModules{
		modules: []appmodule.AppModule{
			genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, txConfig),
			auth.NewAppModule(appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, nil),
			bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper, nil),
			feegrantmodule.NewAppModule(appCodec, app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, app.interfaceRegistry),
			gov.NewAppModule(appCodec, &app.GovKeeper, app.AccountKeeper, app.BankKeeper, nil),
			constitution.NewAppModule(app.ConstitutionKeeper),
			mint.NewAppModule(appCodec, app.MintKeeper, app.AccountKeeper, nil, nil),
			slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil, app.interfaceRegistry),
			distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, nil),
			customstaking.NewAppModule(
				sdkstaking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, nil),
				app.CustomStakingKeeper,
			),
			upgrade.NewAppModule(app.UpgradeKeeper, app.AccountKeeper.AddressCodec()),
			evidence.NewAppModule(app.EvidenceKeeper),
			authzmodule.NewAppModule(appCodec, app.AuthzKeeper, app.AccountKeeper, app.BankKeeper, app.interfaceRegistry),
			consensus.NewAppModule(appCodec, app.ConsensusParamsKeeper),
			vesting.NewAppModule(app.AccountKeeper, app.BankKeeper),
			// IBC modules
			ibc.NewAppModule(app.IBCKeeper),
			ibctm.NewAppModule(tmLightClientModule),
			transfer.NewAppModule(app.TransferKeeper),
			// Cosmos EVM modules
			vmModule,
			feemarket.NewAppModule(app.FeeMarketKeeper),
			erc20.NewAppModule(app.Erc20Keeper, app.AccountKeeper),
		},
		evm: vmModule,
	}
}

type wiredAppModules struct {
	modules []appmodule.AppModule
	evm     vm.AppModule
}

// Keep appModules in the core appmodule.AppModule form and materialize
// the module map expected by module.NewManagerFromMap.
func moduleManagerMap(mods []appmodule.AppModule) map[string]appmodule.AppModule {
	moduleMap := make(map[string]appmodule.AppModule, len(mods))

	for _, m := range mods {
		namedModule, ok := m.(module.HasName)
		if !ok {
			panic("app module does not implement HasName")
		}

		moduleName := namedModule.Name()
		if _, exists := moduleMap[moduleName]; exists {
			panic("duplicate app module name: " + moduleName)
		}
		moduleMap[moduleName] = m
	}

	return moduleMap
}

func newBasicManagerFromManager(app *App) module.BasicManager {
	basicManager := module.NewBasicManagerFromManager(
		app.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName:     genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			stakingtypes.ModuleName:     sdkstaking.AppModuleBasic{},
			govtypes.ModuleName:         gov.NewAppModuleBasic(nil),
			ibctransfertypes.ModuleName: transfer.AppModuleBasic{},
		},
	)
	basicManager.RegisterInterfaces(app.interfaceRegistry)
	return basicManager
}

func ModuleOrderPreBlockers() []string {
	return append([]string(nil), preBlockerOrder...)
}

func ModuleOrderBeginBlockers() []string {
	return append([]string(nil), beginBlockerOrder...)
}

func ModuleOrderEndBlockers() []string {
	return append([]string(nil), endBlockerOrder...)
}

func ModuleOrderInitGenesis() []string {
	return append([]string(nil), initGenesisOrder...)
}
