package types

import "time"

const (
	// DefaultMinTimeoutWindow is the default minimum required remaining timeout window for
	// inbound exchange packets before Guru forwards funds to the destination chain.
	DefaultMinTimeoutWindow = time.Minute
)

var (
	// MinTimeoutWindow is checked against ctx.BlockTime() to keep evaluation deterministic.
	// It is declared as a variable to allow future module-level parameter wiring.
	MinTimeoutWindow = DefaultMinTimeoutWindow
)
