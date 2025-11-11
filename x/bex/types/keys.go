package types

const (
	// module name
	ModuleName = "bex"

	// StoreKey is the default store key for the module
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName
)

// KV Store key prefix bytes
const (
	prefixModeratorAddress = iota + 1
	prefixExchanges
	prefixAdmins
	prefixNextExchangeId
	prefixRatemeter
	prefixAddressRateRegistry
	prefixCollectedFees
)

// KV Store key prefixes
var (
	KeyModeratorAddress    = []byte{prefixModeratorAddress}
	KeyExchanges           = []byte{prefixExchanges}
	KeyAdmins              = []byte{prefixAdmins}
	KeyNextExchangeId      = []byte{prefixNextExchangeId}
	KeyRatemeter           = []byte{prefixRatemeter}
	KeyAddressRateRegistry = []byte{prefixAddressRateRegistry}
	KeyCollectedFees       = []byte{prefixCollectedFees}
)

// default keys
var (
	ExchangeStatusActive   = "active"
	ExchangeStatusInactive = "inactive"

	ExchangeKeyAdminAddress   = "admin_address"
	ExchangeKeyReserveAddress = "reserve_address"
	ExchangeKeyFee            = "fee"
	ExchangeKeyLimit          = "limit"
	ExchangeKeyStatus         = "status"
	ExchangeKeyMetadata       = "metadata"
)
