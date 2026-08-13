package keepers

import (
	"fmt"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	evidencekeeper "cosmossdk.io/x/evidence/keeper"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensusparamkeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	erc20keeper "github.com/cosmos/evm/x/erc20/keeper"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	transferkeeper "github.com/cosmos/ibc-go/v10/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	chainconfig "github.com/gurufinglobal/guru/v2/config"
	constitutionkeeper "github.com/gurufinglobal/guru/v2/x/constitution/keeper"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	oraclekeeper "github.com/gurufinglobal/guru/v2/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
	customstakingkeeper "github.com/gurufinglobal/guru/v2/x/staking/keeper"
)

// AppKeepers owns the stateful dependencies used by the Guru application.
// The IBC transfer dependency is wired so every v0.6.1 default static
// precompile has a complete implementation even when it starts inactive.
type AppKeepers struct {
	keys storeKeys

	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.Keeper
	StakingKeeper         *stakingkeeper.Keeper
	CustomStakingKeeper   *customstakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	MintKeeper            mintkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             govkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	AuthzKeeper           authzkeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	FeeGrantKeeper        feegrantkeeper.Keeper
	ConsensusParamsKeeper consensusparamkeeper.Keeper

	IBCKeeper      *ibckeeper.Keeper
	TransferKeeper transferkeeper.Keeper

	FeeMarketKeeper    feemarketkeeper.Keeper
	FeeMarketAdapter   FeeMarketAdapter
	ConstitutionKeeper constitutionkeeper.Keeper
	OracleKeeper       oraclekeeper.Keeper
	EVMKeeper          *evmkeeper.Keeper
	ERC20Keeper        erc20keeper.Keeper
}

// Config contains the application dependencies and runtime values required to
// construct the keeper graph.
type Config struct {
	Codec              codec.Codec
	LegacyAmino        *codec.LegacyAmino
	BaseApp            *baseapp.BaseApp
	Logger             log.Logger
	HomePath           string
	SkipUpgradeHeights map[int64]bool
	EVMChainID         uint64
	EVMTracer          string
}

// NewAppKeepers constructs the complete Guru keeper graph.
func NewAppKeepers(cfg Config) (*AppKeepers, error) {
	keys := newStoreKeys()
	permissions := moduleAccountPermissions()
	accountCodec := evmaddress.NewEvmCodec(chainconfig.Bech32PrefixAccAddr)
	govAddress := authtypes.NewModuleAddress(govtypes.ModuleName)
	authority, err := accountCodec.BytesToString(govAddress)
	if err != nil {
		return nil, fmt.Errorf("encode governance authority: %w", err)
	}

	keepers := &AppKeepers{keys: keys}
	keepers.ConsensusParamsKeeper = consensusparamkeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(consensusparamtypes.StoreKey)),
		authority,
		runtime.EventService{},
	)
	cfg.BaseApp.SetParamStore(keepers.ConsensusParamsKeeper.ParamsStore)

	keepers.AccountKeeper = authkeeper.NewAccountKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(authtypes.StoreKey)),
		authtypes.ProtoBaseAccount,
		permissions,
		accountCodec,
		chainconfig.Bech32PrefixAccAddr,
		authority,
	)

	blocked, err := blockedBankAddresses(permissions, accountCodec)
	if err != nil {
		return nil, err
	}
	keepers.BankKeeper = bankkeeper.NewBaseKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(banktypes.StoreKey)),
		keepers.AccountKeeper,
		blocked,
		authority,
		cfg.Logger,
	)
	keepers.StakingKeeper = stakingkeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(stakingtypes.StoreKey)),
		keepers.AccountKeeper,
		keepers.BankKeeper,
		authority,
		evmaddress.NewEvmCodec(chainconfig.Bech32PrefixValAddr),
		evmaddress.NewEvmCodec(chainconfig.Bech32PrefixConsAddr),
	)
	keepers.MintKeeper = mintkeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(minttypes.StoreKey)),
		keepers.StakingKeeper,
		keepers.AccountKeeper,
		keepers.BankKeeper,
		authtypes.FeeCollectorName,
		authority,
	)
	keepers.DistrKeeper = distrkeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(distrtypes.StoreKey)),
		keepers.AccountKeeper,
		keepers.BankKeeper,
		keepers.StakingKeeper,
		authtypes.FeeCollectorName,
		authority,
	)
	keepers.SlashingKeeper = slashingkeeper.NewKeeper(
		cfg.Codec,
		cfg.LegacyAmino,
		runtime.NewKVStoreService(keys.getKVStoreKey(slashingtypes.StoreKey)),
		keepers.StakingKeeper,
		authority,
	)
	keepers.StakingKeeper.SetHooks(stakingtypes.NewMultiStakingHooks(
		keepers.DistrKeeper.Hooks(),
		keepers.SlashingKeeper.Hooks(),
	))

	keepers.FeeGrantKeeper = feegrantkeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(feegrant.StoreKey)),
		keepers.AccountKeeper,
	)
	keepers.AuthzKeeper = authzkeeper.NewKeeper(
		runtime.NewKVStoreService(keys.getKVStoreKey(authzkeeper.StoreKey)),
		cfg.Codec,
		cfg.BaseApp.MsgServiceRouter(),
		keepers.AccountKeeper,
	)
	keepers.UpgradeKeeper = upgradekeeper.NewKeeper(
		cfg.SkipUpgradeHeights,
		runtime.NewKVStoreService(keys.getKVStoreKey(upgradetypes.StoreKey)),
		cfg.Codec,
		cfg.HomePath,
		cfg.BaseApp,
		authority,
	)
	keepers.IBCKeeper = ibckeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(ibcexported.StoreKey)),
		nil,
		keepers.UpgradeKeeper,
		authority,
	)

	governanceKeeper := govkeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(govtypes.StoreKey)),
		keepers.AccountKeeper,
		keepers.BankKeeper,
		keepers.StakingKeeper,
		keepers.DistrKeeper,
		cfg.BaseApp.MsgServiceRouter(),
		govtypes.DefaultConfig(),
		authority,
	)
	// No legacy content handlers are installed. A sealed empty router makes
	// legacy proposals fail as unsupported instead of dereferencing a nil router.
	governanceKeeper.SetLegacyRouter(govv1beta1.NewRouter())
	keepers.GovKeeper = *governanceKeeper.SetHooks(govtypes.NewMultiGovHooks())
	evidenceKeeper := evidencekeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(evidencetypes.StoreKey)),
		keepers.StakingKeeper,
		keepers.SlashingKeeper,
		keepers.AccountKeeper.AddressCodec(),
		runtime.ProvideCometInfoService(),
	)
	// Stage C installs no application-defined evidence handlers. A sealed empty
	// router makes submitted custom evidence fail deterministically instead of
	// dereferencing a nil router; CometBFT equivocation handling remains wired
	// through the module's begin-block path.
	evidenceKeeper.SetRouter(evidencetypes.NewRouter())
	keepers.EvidenceKeeper = *evidenceKeeper

	keepers.FeeMarketKeeper = feemarketkeeper.NewKeeper(
		cfg.Codec,
		govAddress,
		keys.getKVStoreKey(feemarkettypes.StoreKey),
		keys.getTransientStoreKey(feemarkettypes.TransientKey),
	)
	keepers.FeeMarketAdapter = newFeeMarketAdapter(keepers.FeeMarketKeeper)
	keepers.ConstitutionKeeper = constitutionkeeper.NewKeeper(
		govAddress,
		runtime.NewKVStoreService(keys.getKVStoreKey(constitutiontypes.StoreKey)),
		cfg.Codec,
		keepers.AccountKeeper.AddressCodec(),
		keepers.BankKeeper,
	)
	keepers.ConstitutionKeeper.SetFeeMarketKeeper(keepers.FeeMarketAdapter)
	keepers.CustomStakingKeeper = customstakingkeeper.NewKeeper(
		keepers.StakingKeeper,
		&keepers.ConstitutionKeeper,
		keepers.AccountKeeper.AddressCodec(),
	)
	keepers.OracleKeeper = oraclekeeper.NewKeeper(
		runtime.NewKVStoreService(keys.getKVStoreKey(oracletypes.StoreKey)),
		cfg.Codec,
		keepers.AccountKeeper.AddressCodec(),
		&keepers.ConstitutionKeeper,
	)
	keepers.OracleKeeper.SetHooks(oracletypes.NewMultiOracleHooks(&keepers.ConstitutionKeeper))

	// DefaultStaticPrecompiles stores pointers to the ERC-20 and transfer keeper
	// fields. The fields are populated below before the application can execute
	// a transaction, completing the circular dependency used by upstream evmd.
	staticPrecompiles := defaultStaticPrecompiles(keepers, cfg.Codec)
	keepers.EVMKeeper = evmkeeper.NewKeeper(
		cfg.Codec,
		keys.getKVStoreKey(evmtypes.StoreKey),
		keys.getTransientStoreKey(evmtypes.TransientKey),
		keys.kv,
		govAddress,
		keepers.AccountKeeper,
		keepers.BankKeeper,
		keepers.StakingKeeper,
		keepers.FeeMarketKeeper,
		&keepers.ConsensusParamsKeeper,
		&keepers.ERC20Keeper,
		cfg.EVMChainID,
		cfg.EVMTracer,
	).WithDefaultEvmCoinInfo(evmtypes.EvmCoinInfo{
		Denom:         chainconfig.BaseDenom,
		ExtendedDenom: chainconfig.BaseDenom,
		DisplayDenom:  chainconfig.DisplayDenom,
		Decimals:      chainconfig.DenomExponent,
	}).WithStaticPrecompiles(staticPrecompiles)
	keepers.ERC20Keeper = erc20keeper.NewKeeper(
		keys.getKVStoreKey(erc20types.StoreKey),
		cfg.Codec,
		govAddress,
		keepers.AccountKeeper,
		keepers.BankKeeper,
		keepers.EVMKeeper,
		keepers.StakingKeeper,
		&keepers.TransferKeeper,
	)
	keepers.TransferKeeper = transferkeeper.NewKeeper(
		cfg.Codec,
		runtime.NewKVStoreService(keys.getKVStoreKey(ibctransfertypes.StoreKey)),
		nil,
		keepers.IBCKeeper.ChannelKeeper,
		keepers.IBCKeeper.ChannelKeeper,
		cfg.BaseApp.MsgServiceRouter(),
		keepers.AccountKeeper,
		keepers.BankKeeper,
		authority,
	)
	keepers.TransferKeeper.SetAddressCodec(accountCodec)

	return keepers, nil
}

// GetKVStoreKeys returns the persistent store keys mounted by the application.
func (keepers *AppKeepers) GetKVStoreKeys() map[string]*storetypes.KVStoreKey {
	return keepers.keys.kv
}

// GetTransientStoreKeys returns the transient store keys mounted by the application.
func (keepers *AppKeepers) GetTransientStoreKeys() map[string]*storetypes.TransientStoreKey {
	return keepers.keys.transient
}
