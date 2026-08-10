// Package app provides the Stage A application composition shell.
package app

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/version"

	"github.com/gurufinglobal/guru/v2/config"
)

// App embeds only a configured BaseApp and serialization boundary. It has no
// stores, keepers, modules, ante handler, or node lifecycle wiring.
type App struct {
	*baseapp.BaseApp
	encoding EncodingConfig
}

// New constructs the Stage A application shell. State loading is rejected
// explicitly until persistent stores and module lifecycle wiring exist.
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

	baseApplication := baseapp.NewBaseApp(
		config.BaseAppName,
		options.Logger,
		options.DB,
		encodingConfig.TxConfig.TxDecoder(),
		options.BaseAppOptions...,
	)
	baseApplication.SetInterfaceRegistry(encodingConfig.InterfaceRegistry)
	baseApplication.SetTxEncoder(encodingConfig.TxConfig.TxEncoder())
	baseApplication.SetVersion(version.Version)
	if options.TraceStore != nil {
		baseApplication.SetCommitMultiStoreTracer(options.TraceStore)
	}

	return &App{
		BaseApp:  baseApplication,
		encoding: encodingConfig,
	}, nil
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

// TxConfig returns the transaction encoder, decoder, and signing handlers.
func (app *App) TxConfig() client.TxConfig {
	return app.encoding.TxConfig
}
