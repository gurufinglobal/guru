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

func newSDKAppCreator() servertypes.AppCreator {
	return func(
		logger log.Logger,
		db dbm.DB,
		traceStore io.Writer,
		appOpts servertypes.AppOptions,
	) servertypes.Application {
		homePath, err := validateRuntimeIdentity(appOpts, false)
		if err != nil {
			panic(fmt.Errorf("validate Guru maintenance identity: %w", err))
		}
		application, err := app.New(newAppOptions(logger, db, traceStore, appOpts, homePath, true))
		if err != nil {
			panic(fmt.Errorf("create Guru maintenance application: %w", err))
		}
		return application
	}
}

// newApp is the process boundary used by the Cosmos EVM server. The server
// creator contract cannot return an error, so invalid operator configuration
// is converted to a startup panic with an actionable message.
func newApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	appOpts servertypes.AppOptions,
) cosmosevmserver.Application {
	homePath, err := validateRuntimeIdentity(appOpts, true)
	if err != nil {
		panic(fmt.Errorf("validate Guru runtime identity: %w", err))
	}
	options := newAppOptions(logger, db, traceStore, appOpts, homePath, true)
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
	homePath, err := validateRuntimeIdentity(appOpts, true)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	application, err := app.New(newAppOptions(
		logger,
		db,
		traceStore,
		appOpts,
		homePath,
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
	homePath string,
	loadLatest bool,
) app.Options {
	return app.Options{
		Logger:         logger,
		DB:             db,
		TraceStore:     traceStore,
		LoadLatest:     loadLatest,
		HomePath:       homePath,
		EVMTracer:      cast.ToString(appOpts.Get(srvflags.EVMTracer)),
		SkipUpgrades:   getSkipUpgradeHeights(appOpts),
		AppOptions:     appOpts,
		BaseAppOptions: sdkserver.DefaultBaseappOptions(appOpts),
	}
}

func validateRuntimeIdentity(appOpts servertypes.AppOptions, requireEVMConfig bool) (string, error) {
	homePath := cast.ToString(appOpts.Get(flags.FlagHome))
	if homePath == "" {
		return "", fmt.Errorf("application home is not configured")
	}

	genesisPath := cast.ToString(appOpts.Get("genesis_file"))
	if genesisPath == "" {
		genesisPath = filepath.Join("config", "genesis.json")
	}
	if !filepath.IsAbs(genesisPath) {
		genesisPath = filepath.Join(homePath, genesisPath)
	}
	genesis, err := genutiltypes.AppGenesisFromFile(genesisPath)
	if err != nil {
		return "", fmt.Errorf("read genesis %q: %w", genesisPath, err)
	}
	if genesis.ChainID != chainconfig.LocalChainID {
		return "", fmt.Errorf(
			"genesis chain ID must be %q, got %q",
			chainconfig.LocalChainID,
			genesis.ChainID,
		)
	}
	configuredChainID := cast.ToString(appOpts.Get(flags.FlagChainID))
	if configuredChainID != "" && configuredChainID != chainconfig.LocalChainID {
		return "", fmt.Errorf(
			"configured chain ID must be %q, got %q",
			chainconfig.LocalChainID,
			configuredChainID,
		)
	}
	rawEVMChainID := appOpts.Get(srvflags.EVMChainID)
	evmChainID := cast.ToUint64(rawEVMChainID)
	// SDK maintenance commands build a narrow AppOptions object without EVM
	// keys. The node and export paths require an explicit matching value;
	// maintenance paths inherit the immutable application value when absent.
	if rawEVMChainID == nil && requireEVMConfig {
		return "", fmt.Errorf("EVM chain ID is not configured")
	}
	if rawEVMChainID != nil && evmChainID != chainconfig.EVMChainID {
		return "", fmt.Errorf(
			"EVM chain ID must be %d, got %d",
			chainconfig.EVMChainID,
			evmChainID,
		)
	}
	return homePath, nil
}

func getSkipUpgradeHeights(appOpts servertypes.AppOptions) map[int64]bool {
	heights := cast.ToIntSlice(appOpts.Get(sdkserver.FlagUnsafeSkipUpgrades))
	result := make(map[int64]bool, len(heights))
	for _, height := range heights {
		result[int64(height)] = true
	}
	return result
}
