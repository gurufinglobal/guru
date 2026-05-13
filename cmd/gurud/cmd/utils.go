package cmd

import (
	"errors"
	"time"

	"cosmossdk.io/log/v2"
	cmtcfg "github.com/cometbft/cometbft/config"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/store/v2"
	snapshottypes "github.com/cosmos/cosmos-sdk/store/v2/snapshots/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	cosmosevmserver "github.com/cosmos/evm/server"
	cosmosevmserverconfig "github.com/cosmos/evm/server/config"
	"github.com/cosmos/evm/utils"
	"github.com/gurufinglobal/guru/v3/app"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func addModuleInitFlags(_ *cobra.Command) {}

func defaultAppToml() (string, any) {
	template := serverconfig.DefaultConfigTemplate + cosmosevmserverconfig.DefaultEVMConfigTemplate

	cfg := cosmosevmserverconfig.DefaultConfig()
	cfg.MinGasPrices = "0" + appparams.BaseDenom
	cfg.EVM.EVMChainID = appparams.EVMChainID
	cfg.API.Enable = true
	cfg.JSONRPC.Enable = true
	cfg.JSONRPC.Address = "0.0.0.0:8545"

	return template, cfg
}

func defaultConfigToml() *cmtcfg.Config {
	cfg := cmtcfg.DefaultConfig()

	cfg.DBBackend = "pebbledb"
	// Krakatoa EVM mempool is enabled by default (mempool.max-txs=0),
	// so CometBFT must use the app-side mempool type.
	cfg.Mempool.Type = cmtcfg.MempoolTypeApp
	cfg.Consensus.TimeoutCommit = 500 * time.Millisecond

	return cfg
}

func newApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions) cosmosevmserver.Application {
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

	return app.NewApp(
		logger, db, true,
		appOpts,
		baseappOptions...,
	)
}

func appExport(
	logger log.Logger,
	db dbm.DB,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	var emptyApp *app.App

	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return servertypes.ExportedApp{}, errors.New("application home not set")
	}

	viperAppOpts, ok := appOpts.(*viper.Viper)
	if !ok {
		return servertypes.ExportedApp{}, errors.New("appOpts is not viper.Viper")
	}

	// overwrite the FlagInvCheckPeriod
	viperAppOpts.Set(sdkserver.FlagInvCheckPeriod, 1)
	appOpts = viperAppOpts

	// get the chain id
	chainID, err := getChainIDFromOpts(appOpts)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	if height != -1 {
		emptyApp = app.NewApp(logger, db, false, appOpts, baseapp.SetChainID(chainID))

		if err := emptyApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	}

	return emptyApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)

}

func getChainIDFromOpts(appOpts servertypes.AppOptions) (chainID string, err error) {
	// Get the chain Id from appOpts
	chainID = cast.ToString(appOpts.Get(flags.FlagChainID))
	if chainID == "" {
		// If not available load from home
		homeDir := cast.ToString(appOpts.Get(flags.FlagHome))
		chainID, err = utils.GetChainIDFromHome(homeDir)
		if err != nil {
			return "", err
		}
	}

	return chainID, err
}
