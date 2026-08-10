package app

import (
	"errors"
	"io"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
)

var (
	ErrMissingLogger = errors.New("application logger is required")
	ErrMissingDB     = errors.New("application database is required")
)

// AppOptions is reserved for the operator-facing server composition. Consensus
// identity is represented by typed Options fields and immutable config values.
type AppOptions interface {
	Get(string) any
}

// Options contains the explicit dependencies of the application composition
// root. Zero values select the immutable Guru defaults.
type Options struct {
	Logger         log.Logger
	DB             dbm.DB
	TraceStore     io.Writer
	LoadLatest     bool
	HomePath       string
	EVMTracer      string
	MaxTxGasWanted uint64
	SkipUpgrades   map[int64]bool
	AppOptions     AppOptions
	BaseAppOptions []func(*baseapp.BaseApp)
}

func (options Options) validate() error {
	if options.Logger == nil {
		return ErrMissingLogger
	}
	if options.DB == nil {
		return ErrMissingDB
	}
	return nil
}
