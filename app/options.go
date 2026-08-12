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

// AppOptions is reserved for the operator-facing server composition.
type AppOptions interface {
	Get(string) any
}

// Options contains the explicit dependencies of the application composition
// root. Empty ChainID and zero EVMChainID select the Guru defaults; callers
// constructing a non-default network must pass both values explicitly.
type Options struct {
	Logger         log.Logger
	DB             dbm.DB
	TraceStore     io.Writer
	LoadLatest     bool
	HomePath       string
	ChainID        string
	EVMChainID     uint64
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
