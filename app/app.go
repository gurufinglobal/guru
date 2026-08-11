// Package app composes the Guru Cosmos SDK and Cosmos EVM state machine.
package app

import (
	"encoding/json"
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	authtxconfig "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	evmante "github.com/cosmos/evm/ante"

	"github.com/gurufinglobal/guru/v2/config"
)

// App is the Guru state machine and the application boundary used by the
// Cosmos SDK and Cosmos EVM servers.
type App struct {
	*baseapp.BaseApp
	encoding EncodingConfig

	*AppKeepers

	ModuleManager      *module.Manager
	BasicModuleManager module.BasicManager
	configurator       module.Configurator
	anteHandler        sdk.AnteHandler

	pendingTxListeners []evmante.PendingTxListener
}

// New constructs a fully wired Cosmos EVM application.
func New(options Options) (*App, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	if err := config.SetupSDKConfig(); err != nil {
		return nil, err
	}

	encodingConfig, err := MakeEncodingConfig()
	if err != nil {
		return nil, err
	}

	baseAppOptions := append([]func(*baseapp.BaseApp){}, options.BaseAppOptions...)
	// Guru delegates transaction storage and gossip to CometBFT. Apply NoOp
	// after all caller options so the SDK app-side mempool cannot be enabled by
	// mempool.max-txs or another server option.
	baseAppOptions = append(baseAppOptions, baseapp.SetMempool(sdkmempool.NoOpMempool{}))
	// The immutable chain ID is applied last so callers cannot accidentally
	// construct a state machine whose signing and InitChain domains diverge.
	baseAppOptions = append(baseAppOptions, baseapp.SetChainID(config.LocalChainID))
	baseApplication := baseapp.NewBaseApp(
		config.BaseAppName,
		options.Logger,
		options.DB,
		encodingConfig.TxConfig.TxDecoder(),
		baseAppOptions...,
	)
	baseApplication.SetInterfaceRegistry(encodingConfig.InterfaceRegistry)
	baseApplication.SetTxEncoder(encodingConfig.TxConfig.TxEncoder())
	baseApplication.SetVersion(version.Version)
	if options.TraceStore != nil {
		baseApplication.SetCommitMultiStoreTracer(options.TraceStore)
	}

	homePath := options.HomePath
	if homePath == "" {
		homePath, err = config.DefaultNodeHome()
		if err != nil {
			return nil, fmt.Errorf("resolve node home: %w", err)
		}
	}
	skipUpgrades := make(map[int64]bool, len(options.SkipUpgrades))
	for height, skip := range options.SkipUpgrades {
		skipUpgrades[height] = skip
	}

	keepers, err := newAppKeepers(keeperConfig{
		codec:              encodingConfig.Codec,
		legacyAmino:        encodingConfig.LegacyAmino,
		baseApp:            baseApplication,
		logger:             options.Logger,
		homePath:           homePath,
		skipUpgradeHeights: skipUpgrades,
		evmTracer:          options.EVMTracer,
	})
	if err != nil {
		return nil, err
	}
	if options.AppOptions != nil {
		if err := baseApplication.RegisterStreamingServices(options.AppOptions, keepers.getKVStoreKeys()); err != nil {
			return nil, fmt.Errorf("register state streaming services: %w", err)
		}
	}

	application := &App{
		BaseApp:    baseApplication,
		encoding:   encodingConfig,
		AppKeepers: keepers,
	}
	textualTxConfig, err := NewTextualTxConfig(
		authtxconfig.NewBankKeeperCoinMetadataQueryFn(application.BankKeeper),
	)
	if err != nil {
		return nil, fmt.Errorf("configure textual transaction signing: %w", err)
	}
	application.encoding.TxConfig = textualTxConfig
	application.SetTxDecoder(textualTxConfig.TxDecoder())
	application.SetTxEncoder(textualTxConfig.TxEncoder())
	tmLightClient := application.configureIBC()
	if err := application.configureModules(tmLightClient); err != nil {
		return nil, err
	}
	application.mountStoresAndHandlers()
	if err := application.configureAnteHandler(options.MaxTxGasWanted); err != nil {
		return nil, err
	}

	if options.LoadLatest {
		if err := application.LoadLatestVersion(); err != nil {
			return nil, fmt.Errorf("load latest application version: %w", err)
		}
	}
	return application, nil
}

func (app *App) mountStoresAndHandlers() {
	app.MountKVStores(app.getKVStoreKeys())
	app.MountTransientStores(app.getTransientStoreKeys())
	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
}

// InitChainer validates the fixed Guru identity and initializes all modules.
func (app *App) InitChainer(
	ctx sdk.Context,
	req *abci.RequestInitChain,
) (response *abci.ResponseInitChain, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = nil
			err = fmt.Errorf("initialize genesis: upstream module panic: %v", recovered)
		}
	}()

	if req == nil {
		return nil, fmt.Errorf("init chain request cannot be nil")
	}
	if req.ChainId != config.LocalChainID {
		return nil, fmt.Errorf("CometBFT chain ID must be %q, got %q", config.LocalChainID, req.ChainId)
	}

	var genesis GenesisState
	if err := json.Unmarshal(req.AppStateBytes, &genesis); err != nil {
		return nil, fmt.Errorf("decode genesis: %w", err)
	}
	if err := app.ValidateGenesis(genesis); err != nil {
		return nil, err
	}
	if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
		return nil, fmt.Errorf("set module version map: %w", err)
	}
	return app.ModuleManager.InitGenesis(ctx, app.AppCodec(), genesis)
}

// PreBlocker delegates the consensus-critical pre-block lifecycle.
func (app *App) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	return app.ModuleManager.PreBlock(ctx)
}

// BeginBlocker delegates the module begin-block lifecycle.
func (app *App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

// EndBlocker delegates the module end-block lifecycle.
func (app *App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.ModuleManager.EndBlock(ctx)
}

// Configurator exposes the service registration boundary.
func (app *App) Configurator() module.Configurator {
	return app.configurator
}

// AppCodec returns the protobuf application codec.
func (app *App) AppCodec() codec.Codec {
	return app.encoding.Codec
}

// LegacyAmino returns the legacy Amino codec required by SDK client plumbing.
func (app *App) LegacyAmino() *codec.LegacyAmino {
	return app.encoding.LegacyAmino
}

// InterfaceRegistry returns the Guru address-aware interface registry.
func (app *App) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.encoding.InterfaceRegistry
}

// TxConfig preserves the Cosmos SDK simulation accessor contract.
func (app *App) TxConfig() client.TxConfig {
	return app.GetTxConfig()
}

// GetTxConfig returns the transaction encoder, decoder, and signing handlers.
func (app *App) GetTxConfig() client.TxConfig {
	return app.encoding.TxConfig
}
