package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	snapshottypes "cosmossdk.io/store/snapshots/types"
	storetypes "cosmossdk.io/store/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	cosmosevmserver "github.com/cosmos/evm/server"
	cosmosevmserverconfig "github.com/cosmos/evm/server/config"
	"github.com/gurufinglobal/guru/v3/app"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func addModuleInitFlags(_ *cobra.Command) {}

const (
	defaultCosmosMempoolMaxTxs  = 5_000
	defaultOracleConfigTemplate = `
[oracle]

# Enables local validator oracle vote-extension participation.
enabled = {{ .Oracle.Enabled }}

# Unix domain socket used by validator nodes to query the oracle sidecar.
sidecar_socket = "{{ .Oracle.SidecarSocket }}"

# Timeout for one oracle sidecar request during ExtendVote.
sidecar_timeout = "{{ .Oracle.SidecarTimeout }}"
`
)

type guruConfig struct {
	cosmosevmserverconfig.Config `mapstructure:",squash"`

	Oracle oracleConfig `mapstructure:"oracle"`
}

type oracleConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	SidecarSocket  string `mapstructure:"sidecar_socket"`
	SidecarTimeout string `mapstructure:"sidecar_timeout"`
}

func defaultAppToml() (string, any) {
	template := serverconfig.DefaultConfigTemplate + cosmosevmserverconfig.DefaultEVMConfigTemplate + defaultOracleConfigTemplate

	cfg := cosmosevmserverconfig.DefaultConfig()
	cfg.MinGasPrices = "0" + appparams.BaseDenom
	// Cosmos EVM v0.6.1 uses this SDK setting for the Cosmos side of its
	// unified app mempool. Keep it bounded and enabled; -1 drops Cosmos txs.
	cfg.Mempool.MaxTxs = defaultCosmosMempoolMaxTxs
	// The operator-run oracle reconcile command compares configured feeds with
	// active tasks through the node's gRPC query service.
	cfg.GRPC.Enable = true
	cfg.EVM.EVMChainID = appparams.EVMChainID
	cfg.API.Enable = true
	cfg.JSONRPC.Enable = true
	cfg.JSONRPC.Address = "0.0.0.0:8545"
	// Never permit personal account unlocking from JSON-RPC by default.
	cfg.JSONRPC.AllowInsecureUnlock = false

	return template, guruConfig{
		Config: *cfg,
		Oracle: oracleConfig{
			Enabled:        true,
			SidecarSocket:  "",
			SidecarTimeout: "200ms",
		},
	}
}

func defaultConfigToml() *cmtcfg.Config {
	cfg := cmtcfg.DefaultConfig()

	// The v0.6.1 runtime only registers GoLevelDB and MemDB in the chain
	// binary's application DB path. Use GoLevelDB for both CometBFT and the
	// app-side fallback so a freshly generated home is immediately runnable.
	cfg.DBBackend = "goleveldb"
	// Keep the standard CometBFT transaction gossip path on the v0.6 launch
	// profile. The v0.7 upgrade runbook switches this to the app mempool.
	cfg.Mempool.Type = cmtcfg.MempoolTypeFlood
	cfg.Consensus.TimeoutCommit = 500 * time.Millisecond

	return cfg
}

func newApp(logger log.Logger, db dbm.DB, traceWriter io.Writer, appOpts servertypes.AppOptions) cosmosevmserver.Application {
	var cache storetypes.MultiStorePersistentCache

	if cast.ToBool(appOpts.Get(sdkserver.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	pruningOpts, err := sdkserver.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}

	// get the chain id
	chainID, err := getChainIDFromOpts(appOpts)
	if err != nil {
		panic(err)
	}

	snapshotStore, err := sdkserver.GetSnapshotStore(appOpts)
	if err != nil {
		panic(err)
	}

	snapshotOptions := snapshottypes.NewSnapshotOptions(
		cast.ToUint64(appOpts.Get(sdkserver.FlagStateSyncSnapshotInterval)),
		cast.ToUint32(appOpts.Get(sdkserver.FlagStateSyncSnapshotKeepRecent)),
	)

	baseappOptions := []func(*baseapp.BaseApp){
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(sdkserver.FlagMinGasPrices))),
		baseapp.SetQueryGasLimit(cast.ToUint64(appOpts.Get(sdkserver.FlagQueryGasLimit))),
		baseapp.SetHaltHeight(cast.ToUint64(appOpts.Get(sdkserver.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOpts.Get(sdkserver.FlagHaltTime))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(sdkserver.FlagMinRetainBlocks))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOpts.Get(sdkserver.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOpts.Get(sdkserver.FlagIndexEvents))),
		baseapp.SetSnapshot(snapshotStore, snapshotOptions),
		baseapp.SetIAVLCacheSize(cast.ToInt(appOpts.Get(sdkserver.FlagIAVLCacheSize))),
		baseapp.SetIAVLDisableFastNode(cast.ToBool(appOpts.Get(sdkserver.FlagDisableIAVLFastNode))),
		baseapp.SetChainID(chainID),
	}
	if traceWriter != nil {
		baseappOptions = append(baseappOptions, func(baseApp *baseapp.BaseApp) {
			baseApp.SetCommitMultiStoreTracer(traceWriter)
		})
	}

	return app.NewApp(
		logger, db, true,
		appOpts,
		baseappOptions...,
	)
}

func appExport(
	logger log.Logger,
	db dbm.DB,
	traceWriter io.Writer,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (exported servertypes.ExportedApp, err error) {
	viperAppOpts, ok := appOpts.(*viper.Viper)
	if !ok {
		return servertypes.ExportedApp{}, errors.New("appOpts is not viper.Viper")
	}
	// ExportCmd writes the genesis document to stdout. Cosmos SDK constructs
	// its server logger on that same writer, so module initialization logs would
	// otherwise corrupt the JSON stream. Preserve the configured format/level
	// while routing only export-time application logs to stderr.
	logger, err = sdkserver.CreateSDKLogger(
		sdkserver.NewContext(viperAppOpts, cmtcfg.DefaultConfig(), logger),
		os.Stderr,
	)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return servertypes.ExportedApp{}, errors.New("application home not set")
	}

	// overwrite the FlagInvCheckPeriod
	viperAppOpts.Set(sdkserver.FlagInvCheckPeriod, 1)
	appOpts = viperAppOpts

	// get the chain id
	chainID, err := getChainIDFromOpts(appOpts)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	baseappOptions := []func(*baseapp.BaseApp){baseapp.SetChainID(chainID)}
	if traceWriter != nil {
		baseappOptions = append(baseappOptions, func(baseApp *baseapp.BaseApp) {
			baseApp.SetCommitMultiStoreTracer(traceWriter)
		})
	}
	emptyApp := app.NewApp(logger, db, false, appOpts, baseappOptions...)
	defer func() {
		err = errors.Join(err, emptyApp.Close())
	}()

	if height != -1 {
		if err := emptyApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else if err := emptyApp.LoadLatestVersion(); err != nil {
		return servertypes.ExportedApp{}, err
	}

	return emptyApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}

func getChainIDFromOpts(appOpts servertypes.AppOptions) (chainID string, err error) {
	homeDir := strings.TrimSpace(cast.ToString(appOpts.Get(flags.FlagHome)))
	if homeDir == "" {
		return "", errors.New("application home not set")
	}

	genesisPath := filepath.Join(homeDir, "config", "genesis.json")
	genesis, err := genutiltypes.AppGenesisFromFile(genesisPath)
	if err != nil {
		return "", fmt.Errorf("load chain ID from genesis: %w", err)
	}
	genesisChainID := strings.TrimSpace(genesis.ChainID)
	if genesisChainID == "" {
		return "", fmt.Errorf("genesis at %s has an empty chain ID", genesisPath)
	}

	configuredChainID := strings.TrimSpace(cast.ToString(appOpts.Get(flags.FlagChainID)))
	if configuredChainID != "" && configuredChainID != genesisChainID {
		return "", fmt.Errorf(
			"configured chain ID %q does not match genesis chain ID %q",
			configuredChainID,
			genesisChainID,
		)
	}

	return genesisChainID, nil
}
