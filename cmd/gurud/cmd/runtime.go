package cmd

import (
	"fmt"
	"io"
	"path/filepath"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	cosmosevmserver "github.com/cosmos/evm/server"
	srvflags "github.com/cosmos/evm/server/flags"
	"github.com/spf13/cast"

	"github.com/gurufinglobal/guru/v2/app"
	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

type runtimeConfig struct {
	homePath   string
	chainID    string
	evmChainID uint64
}

func newSDKAppCreator() servertypes.AppCreator {
	return func(
		logger log.Logger,
		db dbm.DB,
		traceStore io.Writer,
		appOpts servertypes.AppOptions,
	) servertypes.Application {
		cfg, err := resolveRuntimeConfig(appOpts)
		if err != nil {
			panic(fmt.Errorf("resolve Guru maintenance configuration: %w", err))
		}
		application, err := app.New(newAppOptions(logger, db, traceStore, appOpts, cfg, true))
		if err != nil {
			panic(fmt.Errorf("create Guru maintenance application: %w", err))
		}
		return application
	}
}

// newApp is the process boundary used by the Cosmos EVM server. The server
// creator contract cannot return an error, so invalid local configuration is
// converted to a startup panic with an actionable message.
func newApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	appOpts servertypes.AppOptions,
) cosmosevmserver.Application {
	cfg, err := resolveRuntimeConfig(appOpts)
	if err != nil {
		panic(fmt.Errorf("resolve Guru runtime configuration: %w", err))
	}
	options := newAppOptions(logger, db, traceStore, appOpts, cfg, true)
	options.MaxTxGasWanted = cast.ToUint64(appOpts.Get(srvflags.EVMMaxTxGasWanted))
	application, err := app.New(options)
	if err != nil {
		panic(fmt.Errorf("create Guru application: %w", err))
	}
	return application
}

func appExport(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	cfg, err := resolveRuntimeConfig(appOpts)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	application, err := app.New(newAppOptions(
		logger,
		db,
		traceStore,
		appOpts,
		cfg,
		height == -1,
	))
	if err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("create application for export: %w", err)
	}
	defer application.Close() //nolint:errcheck

	if height != -1 {
		if err := application.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, fmt.Errorf("load export height %d: %w", height, err)
		}
	}
	return application.ExportAppStateAndValidators(
		forZeroHeight,
		jailAllowedAddrs,
		modulesToExport,
	)
}

func newAppOptions(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	appOpts servertypes.AppOptions,
	cfg runtimeConfig,
	loadLatest bool,
) app.Options {
	return app.Options{
		Logger:         logger,
		DB:             db,
		TraceStore:     traceStore,
		LoadLatest:     loadLatest,
		HomePath:       cfg.homePath,
		ChainID:        cfg.chainID,
		EVMChainID:     cfg.evmChainID,
		EVMTracer:      cast.ToString(appOpts.Get(srvflags.EVMTracer)),
		SkipUpgrades:   getSkipUpgradeHeights(appOpts),
		AppOptions:     appOpts,
		BaseAppOptions: sdkserver.DefaultBaseappOptions(appOpts),
	}
}

// resolveRuntimeConfig follows the upstream server precedence rules. Cosmos
// chain ID comes from an explicit flag when present, otherwise from genesis;
// EVM chain ID comes from app.toml/flags and falls back to Guru's default.
// These are local network selections and are not persisted as Guru-owned state.
func resolveRuntimeConfig(appOpts servertypes.AppOptions) (runtimeConfig, error) {
	homePath := cast.ToString(appOpts.Get(flags.FlagHome))
	if homePath == "" {
		return runtimeConfig{}, fmt.Errorf("application home is not configured")
	}

	chainID := cast.ToString(appOpts.Get(flags.FlagChainID))
	if chainID == "" {
		genesisPath := cast.ToString(appOpts.Get("genesis_file"))
		if genesisPath == "" {
			genesisPath = filepath.Join("config", "genesis.json")
		}
		if !filepath.IsAbs(genesisPath) {
			genesisPath = filepath.Join(homePath, genesisPath)
		}
		genesis, err := genutiltypes.AppGenesisFromFile(genesisPath)
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("read genesis %q: %w", genesisPath, err)
		}
		chainID = genesis.ChainID
	}
	if chainID == "" {
		chainID = chainconfig.DefaultChainID
	}

	evmChainID := cast.ToUint64(appOpts.Get(srvflags.EVMChainID))
	if evmChainID == 0 {
		evmChainID = chainconfig.DefaultEVMChainID
	}

	return runtimeConfig{
		homePath:   homePath,
		chainID:    chainID,
		evmChainID: evmChainID,
	}, nil
}

func getSkipUpgradeHeights(appOpts servertypes.AppOptions) map[int64]bool {
	heights := cast.ToIntSlice(appOpts.Get(sdkserver.FlagUnsafeSkipUpgrades))
	result := make(map[int64]bool, len(heights))
	for _, height := range heights {
		result[int64(height)] = true
	}
	return result
}
