package app

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
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

// AppKeepers owns the stateful dependencies used by the Guru application.
// ERC-20 conversion, IBC applications, and stateful Cosmos precompiles are not
// part of the Stage C composition.
type AppKeepers struct {
	keys storeKeys

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

	// IBCKeeper is wired for the upstream Cosmos EVM ante handler. Stage C does
	// not expose an IBC application route such as ICS-20 transfer.
	IBCKeeper *ibckeeper.Keeper

	FeeMarketKeeper feemarketkeeper.Keeper
	EVMKeeper       *evmkeeper.Keeper

	installedPrecompiles installedStaticPrecompiles
}

type keeperConfig struct {
	codec              codec.Codec
	legacyAmino        *codec.LegacyAmino
	baseApp            *baseapp.BaseApp
	logger             log.Logger
	homePath           string
	skipUpgradeHeights map[int64]bool
	evmTracer          string
}

func newAppKeepers(cfg keeperConfig) (*AppKeepers, error) {
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
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(consensusparamtypes.StoreKey)),
		authority,
		runtime.EventService{},
	)
	cfg.baseApp.SetParamStore(keepers.ConsensusParamsKeeper.ParamsStore)

	keepers.AccountKeeper = authkeeper.NewAccountKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(authtypes.StoreKey)),
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
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(banktypes.StoreKey)),
		keepers.AccountKeeper,
		blocked,
		authority,
		cfg.logger,
	)
	keepers.StakingKeeper = stakingkeeper.NewKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(stakingtypes.StoreKey)),
		keepers.AccountKeeper,
		keepers.BankKeeper,
		authority,
		evmaddress.NewEvmCodec(chainconfig.Bech32PrefixValAddr),
		evmaddress.NewEvmCodec(chainconfig.Bech32PrefixConsAddr),
	)
	keepers.MintKeeper = mintkeeper.NewKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(minttypes.StoreKey)),
		keepers.StakingKeeper,
		keepers.AccountKeeper,
		keepers.BankKeeper,
		authtypes.FeeCollectorName,
		authority,
	)
	keepers.DistrKeeper = distrkeeper.NewKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(distrtypes.StoreKey)),
		keepers.AccountKeeper,
		keepers.BankKeeper,
		keepers.StakingKeeper,
		authtypes.FeeCollectorName,
		authority,
	)
	keepers.SlashingKeeper = slashingkeeper.NewKeeper(
		cfg.codec,
		cfg.legacyAmino,
		runtime.NewKVStoreService(keys.kvKey(slashingtypes.StoreKey)),
		keepers.StakingKeeper,
		authority,
	)
	keepers.StakingKeeper.SetHooks(stakingtypes.NewMultiStakingHooks(
		keepers.DistrKeeper.Hooks(),
		keepers.SlashingKeeper.Hooks(),
	))

	keepers.FeeGrantKeeper = feegrantkeeper.NewKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(feegrant.StoreKey)),
		keepers.AccountKeeper,
	)
	keepers.AuthzKeeper = authzkeeper.NewKeeper(
		runtime.NewKVStoreService(keys.kvKey(authzkeeper.StoreKey)),
		cfg.codec,
		cfg.baseApp.MsgServiceRouter(),
		keepers.AccountKeeper,
	)
	keepers.UpgradeKeeper = upgradekeeper.NewKeeper(
		cfg.skipUpgradeHeights,
		runtime.NewKVStoreService(keys.kvKey(upgradetypes.StoreKey)),
		cfg.codec,
		cfg.homePath,
		cfg.baseApp,
		authority,
	)
	keepers.IBCKeeper = ibckeeper.NewKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(ibcexported.StoreKey)),
		nil,
		keepers.UpgradeKeeper,
		authority,
	)

	governanceKeeper := govkeeper.NewKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(govtypes.StoreKey)),
		keepers.AccountKeeper,
		keepers.BankKeeper,
		keepers.StakingKeeper,
		keepers.DistrKeeper,
		cfg.baseApp.MsgServiceRouter(),
		govtypes.DefaultConfig(),
		authority,
	)
	// No legacy content handlers are installed. A sealed empty router makes
	// legacy proposals fail as unsupported instead of dereferencing a nil router.
	governanceKeeper.SetLegacyRouter(govv1beta1.NewRouter())
	keepers.GovKeeper = *governanceKeeper.SetHooks(govtypes.NewMultiGovHooks())
	keepers.EvidenceKeeper = *evidencekeeper.NewKeeper(
		cfg.codec,
		runtime.NewKVStoreService(keys.kvKey(evidencetypes.StoreKey)),
		keepers.StakingKeeper,
		keepers.SlashingKeeper,
		keepers.AccountKeeper.AddressCodec(),
		runtime.ProvideCometInfoService(),
	)

	keepers.FeeMarketKeeper = feemarketkeeper.NewKeeper(
		cfg.codec,
		govAddress,
		keys.kvKey(feemarkettypes.StoreKey),
		keys.transientKey(feemarkettypes.TransientKey),
	)

	staticPrecompiles := stageCStaticPrecompiles()
	keepers.installedPrecompiles = snapshotStaticPrecompiles(staticPrecompiles)
	keepers.EVMKeeper = evmkeeper.NewKeeper(
		cfg.codec,
		keys.kvKey(evmtypes.StoreKey),
		keys.transientKey(evmtypes.TransientKey),
		keys.kv,
		govAddress,
		keepers.AccountKeeper,
		keepers.BankKeeper,
		keepers.StakingKeeper,
		keepers.FeeMarketKeeper,
		&keepers.ConsensusParamsKeeper,
		nil,
		chainconfig.EVMChainID,
		cfg.evmTracer,
	).WithDefaultEvmCoinInfo(evmtypes.EvmCoinInfo{
		Denom:         chainconfig.BaseDenom,
		ExtendedDenom: chainconfig.BaseDenom,
		DisplayDenom:  chainconfig.DisplayDenom,
		Decimals:      chainconfig.DenomExponent,
	}).WithStaticPrecompiles(staticPrecompiles)

	return keepers, nil
}

func (keepers *AppKeepers) kvStoreKeys() map[string]*storetypes.KVStoreKey {
	return keepers.keys.kv
}

func (keepers *AppKeepers) transientStoreKeys() map[string]*storetypes.TransientStoreKey {
	return keepers.keys.transient
}
