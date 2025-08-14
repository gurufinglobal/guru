package types

const (
	// module name
	ModuleName = "feepolicy"

	// StoreKey is the default store key for the module
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// TransientKey is the key to access the FeePolicy transient store, that is reset
	// during the Commit phase.
	TransientKey = "transient_" + ModuleName
)

// KV Store key prefix bytes
const (
	prefixModeratorAddress = iota + 1
	prefixDiscounts
)

// KV Store key prefixes
var (
	KeyModeratorAddress = []byte{prefixModeratorAddress}
	KeyDiscounts        = []byte{prefixDiscounts}
)
