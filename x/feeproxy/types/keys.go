package types

import "fmt"

const (
	// ModuleName defines the module name.
	ModuleName = "feeproxy"

	// StoreKey is the default store key for the module.
	StoreKey = ModuleName

	// EscrowModuleName is a dedicated module account name used to temporarily
	// lock forwarding fees until the forwarded packet is finalized (ack/timeout).
	//
	// IMPORTANT: This account is not a treasury. Funds must be settled (success)
	// or returned into the transfer escrow (failure) by feeproxy IBC middleware.
	EscrowModuleName = "feeproxy_escrow"

	// LockedFeeKeyPrefix is the prefix for per-packet locked fee records.
	LockedFeeKeyPrefix = "locked_fee"
)

// LockedFeeKey is keyed by {portID, channelID, sequence} for an outgoing IBC packet.
func LockedFeeKey(portID, channelID string, sequence uint64) []byte {
	// Use a stable, human-readable key layout (similar to IBC core prefixes).
	return []byte(fmt.Sprintf("%s/%s/%s/%d", LockedFeeKeyPrefix, portID, channelID, sequence))
}

