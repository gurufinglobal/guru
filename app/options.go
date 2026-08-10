package app

import (
	"errors"
	"io"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
)

var (
	ErrMissingLogger         = errors.New("application logger is required")
	ErrMissingDB             = errors.New("application database is required")
	ErrLoadLatestUnsupported = errors.New("loading application state is not available in Stage A")
)

// AppOptions is the future runtime-configuration seam. Stage A deliberately
// does not read options because no store, keeper, or server is wired.
type AppOptions interface {
	Get(string) any
}

// Options contains the explicit dependencies of the Stage A composition root.
type Options struct {
	Logger         log.Logger
	DB             dbm.DB
	TraceStore     io.Writer
	LoadLatest     bool
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
	if options.LoadLatest {
		return ErrLoadLatestUnsupported
	}

	return nil
}
