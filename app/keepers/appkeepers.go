package keepers

import (
	"fmt"
	"sort"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensusparamkeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	precompiletypes "github.com/cosmos/evm/precompiles/types"
	erc20keeper "github.com/cosmos/evm/x/erc20/keeper"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	ibccallbackskeeper "github.com/cosmos/evm/x/ibc/callbacks/keeper"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	transferkeeper "github.com/cosmos/ibc-go/v11/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v11/modules/core/keeper"
	"github.com/ethereum/go-ethereum/common"
	corevm "github.com/ethereum/go-ethereum/core/vm"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

type AppKeepers struct {
	kvKeys  map[string]*storetypes.KVStoreKey
	objKeys map[string]*storetypes.ObjectStoreKey

	// cosmos sdk keepers
	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.Keeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	MintKeeper            mintkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             govkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	AuthzKeeper           authzkeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	FeeGrantKeeper        feegrantkeeper.Keeper
	ConsensusParamsKeeper consensusparamkeeper.Keeper

	// cosmos evm keepers
	FeeMarketKeeper feemarketkeeper.Keeper
	EVMKeeper       *evmkeeper.Keeper
	Erc20Keeper     erc20keeper.Keeper

	// IBC keepers
	IBCKeeper      *ibckeeper.Keeper // IBC Keeper must be a pointer in the app, so we can SetRouter on it correctly
	TransferKeeper *transferkeeper.Keeper
	CallbackKeeper ibccallbackskeeper.ContractKeeper

	// guru keepers
	// ...
}

func NewAppKeepers(cfg appparams.KeepersInitConfig) *AppKeepers {
	appKeepers := &AppKeepers{}

	// Set keys KVStoreKey, ObjectStoreKey
	appKeepers.GenerateKeys()

	if cfg.BaseApp == nil {
		panic("base app cannot be nil")
	}

	govAddress := authtypes.NewModuleAddress(govtypes.ModuleName)
	addressCodec := evmaddress.NewEvmCodec(cfg.AccountAddressPrefix)
	authority, err := addressCodec.BytesToString(govAddress)
	if err != nil {
		panic(fmt.Errorf("failed to convert gov address to string: %v", err))
	}
	
	moduleAccountPerms := cfg.ModuleAccountPerms
	if len(moduleAccountPerms) == 0 {
		panic("module account permissions cannot be empty")
	}

	appKeepers.ConsensusParamsKeeper = consensusparamkeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[consensusparamtypes.StoreKey]),
		authority,
		runtime.EventService{},
	)
	cfg.BaseApp.SetParamStore(appKeepers.ConsensusParamsKeeper.ParamsStore)

	appKeepers.AccountKeeper = authkeeper.NewAccountKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		moduleAccountPerms,
		evmaddress.NewEvmCodec(cfg.AccountAddressPrefix),
		cfg.AccountAddressPrefix,
		authority,
	)

	blockedAddrs := make(map[string]bool)
	modules := make([]string, 0, len(moduleAccountPerms))
	for module := range moduleAccountPerms {
		modules = append(modules, module)
	}
	sort.Strings(modules)

	for _, module := range modules {
		moduleAddress := authtypes.NewModuleAddress(module)
		moduleAddressString, err := addressCodec.BytesToString(moduleAddress)
		if err != nil {
			panic(fmt.Errorf("failed to convert module address to string: %v", err))
		}
		blockedAddrs[moduleAddressString] = true
	}

	blockedPrecompilesHex := append([]string{}, evmtypes.AvailableStaticPrecompiles...)
	for _, addr := range corevm.PrecompiledAddressesPrague {
		blockedPrecompilesHex = append(blockedPrecompilesHex, addr.Hex())
	}

	for _, precompile := range blockedPrecompilesHex {
		precompileAddress := common.HexToAddress(precompile)
		precompileAddressString, err := addressCodec.BytesToString(precompileAddress.Bytes())
		if err != nil {
			panic(fmt.Errorf("failed to convert precompile address to string: %v", err))
		}
		blockedAddrs[precompileAddressString] = true
	}

	appKeepers.BankKeeper = bankkeeper.NewBaseKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[banktypes.StoreKey]),
		appKeepers.AccountKeeper,
		blockedAddrs,
		authority,
		cfg.Logger,
	)
	appKeepers.BankKeeper = appKeepers.BankKeeper.WithObjStoreKey(appKeepers.objKeys[banktypes.ObjectStoreKey])

	appKeepers.StakingKeeper = stakingkeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[stakingtypes.StoreKey]),
		appKeepers.AccountKeeper,
		appKeepers.BankKeeper,
		authority,
		evmaddress.NewEvmCodec(cfg.ValidatorAddressPrefix),
		evmaddress.NewEvmCodec(cfg.ConsensusAddressPrefix),
	)

	appKeepers.MintKeeper = mintkeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[minttypes.StoreKey]),
		appKeepers.StakingKeeper,
		appKeepers.AccountKeeper,
		appKeepers.BankKeeper,
		authtypes.FeeCollectorName,
		authority,
	)

	appKeepers.DistrKeeper = distrkeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[distrtypes.StoreKey]),
		appKeepers.AccountKeeper,
		appKeepers.BankKeeper,
		appKeepers.StakingKeeper,
		authtypes.FeeCollectorName,
		authority,
	)

	appKeepers.SlashingKeeper = slashingkeeper.NewKeeper(
		cfg.AppCodec,
		nil,
		runtime.NewKVStoreService(appKeepers.kvKeys[slashingtypes.StoreKey]),
		appKeepers.StakingKeeper,
		authority,
	)

	appKeepers.StakingKeeper.SetHooks(
		stakingtypes.NewMultiStakingHooks(appKeepers.DistrKeeper.Hooks(), appKeepers.SlashingKeeper.Hooks()),
	)

	appKeepers.FeeGrantKeeper = feegrantkeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[feegrant.StoreKey]),
		appKeepers.AccountKeeper,
	)

	appKeepers.AuthzKeeper = authzkeeper.NewKeeper(
		runtime.NewKVStoreService(appKeepers.kvKeys[authzkeeper.StoreKey]),
		cfg.AppCodec,
		cfg.BaseApp.MsgServiceRouter(),
		appKeepers.AccountKeeper,
	)

	appKeepers.UpgradeKeeper = upgradekeeper.NewKeeper(
		cfg.SkipUpgradeHeights,
		runtime.NewKVStoreService(appKeepers.kvKeys[upgradetypes.StoreKey]),
		cfg.AppCodec,
		cfg.HomePath,
		cfg.BaseApp,
		authority,
	)

	appKeepers.IBCKeeper = ibckeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[ibcexported.StoreKey]),
		appKeepers.UpgradeKeeper,
		authority,
	)

	govKeeper := govkeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[govtypes.StoreKey]),
		appKeepers.AccountKeeper,
		appKeepers.BankKeeper,
		appKeepers.DistrKeeper,
		cfg.BaseApp.MsgServiceRouter(),
		govtypes.DefaultConfig(),
		authority,
		govkeeper.NewDefaultCalculateVoteResultsAndVotingPower(appKeepers.StakingKeeper),
	)
	appKeepers.GovKeeper = *govKeeper.SetHooks(govtypes.NewMultiGovHooks())

	evidenceKeeper := evidencekeeper.NewKeeper(
		cfg.AppCodec,
		runtime.NewKVStoreService(appKeepers.kvKeys[evidencetypes.StoreKey]),
		appKeepers.StakingKeeper,
		appKeepers.SlashingKeeper,
		appKeepers.AccountKeeper.AddressCodec(),
		runtime.ProvideCometInfoService(),
	)
	appKeepers.EvidenceKeeper = *evidenceKeeper

	appKeepers.FeeMarketKeeper = feemarketkeeper.NewKeeper(
		cfg.AppCodec,
		authtypes.NewModuleAddress(govtypes.ModuleName),
		appKeepers.kvKeys[feemarkettypes.StoreKey],
	)

	appKeepers.TransferKeeper = transferkeeper.NewKeeper(
		cfg.AppCodec,
		evmaddress.NewEvmCodec(cfg.AccountAddressPrefix),
		runtime.NewKVStoreService(appKeepers.kvKeys[ibctransfertypes.StoreKey]),
		appKeepers.IBCKeeper.ChannelKeeper,
		cfg.BaseApp.MsgServiceRouter(),
		appKeepers.AccountKeeper,
		appKeepers.BankKeeper,
		authority,
	)

	nonTransientKeys := appKeepers.GetNonTransientKeys()
	appKeepers.EVMKeeper = evmkeeper.NewKeeper(
		cfg.AppCodec,
		appKeepers.kvKeys[evmtypes.StoreKey],
		appKeepers.objKeys[evmtypes.ObjectKey],
		nonTransientKeys,
		authtypes.NewModuleAddress(govtypes.ModuleName),
		appKeepers.AccountKeeper,
		appKeepers.BankKeeper,
		appKeepers.StakingKeeper,
		appKeepers.FeeMarketKeeper,
		&appKeepers.ConsensusParamsKeeper,
		&appKeepers.Erc20Keeper,
		cfg.EVMChainID,
		cfg.EVMTracer,
	).WithStaticPrecompiles(
		precompiletypes.DefaultStaticPrecompiles(
			*appKeepers.StakingKeeper,
			appKeepers.DistrKeeper,
			appKeepers.BankKeeper,
			&appKeepers.Erc20Keeper,
			appKeepers.TransferKeeper,
			appKeepers.IBCKeeper.ChannelKeeper,
			appKeepers.IBCKeeper.ClientKeeper,
			appKeepers.GovKeeper,
			appKeepers.SlashingKeeper,
			cfg.AppCodec,
			precompiletypes.WithAddressCodec(evmaddress.NewEvmCodec(cfg.AccountAddressPrefix)),
			precompiletypes.WithValidatorAddrCodec(evmaddress.NewEvmCodec(cfg.ValidatorAddressPrefix)),
			precompiletypes.WithConsensusAddrCodec(evmaddress.NewEvmCodec(cfg.ConsensusAddressPrefix)),
		),
	)
	appKeepers.EVMKeeper.EnableVirtualFeeCollection()

	appKeepers.Erc20Keeper = erc20keeper.NewKeeper(
		appKeepers.kvKeys[erc20types.StoreKey],
		cfg.AppCodec,
		authtypes.NewModuleAddress(govtypes.ModuleName),
		appKeepers.AccountKeeper,
		appKeepers.BankKeeper,
		appKeepers.EVMKeeper,
		appKeepers.StakingKeeper,
		appKeepers.TransferKeeper,
	)

	appKeepers.CallbackKeeper = ibccallbackskeeper.NewKeeper(
		appKeepers.AccountKeeper,
		appKeepers.EVMKeeper,
		appKeepers.Erc20Keeper,
	)

	return appKeepers
}
