package types

import "time"

const (
	// DefaultMinTimeoutWindow is the default timeout window used by the
	// IBC v2 exchange fallback policy.
	DefaultMinTimeoutWindow = 10 * time.Minute
)

// MinTimeoutWindow is checked against ctx.BlockTime() to keep evaluation deterministic.
// It is declared as a variable to allow future module-level parameter wiring.
var MinTimeoutWindow = DefaultMinTimeoutWindow
