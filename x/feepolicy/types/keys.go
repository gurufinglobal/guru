package types

import "cosmossdk.io/collections"

const (
	ModuleName = "feepolicy"
	StoreKey   = ModuleName
	RouterKey  = ModuleName
)

var (
	// LegacyModeratorKey is permanently reserved for the retired module-local
	// moderator record. Constitution owns moderator state; feepolicy never reads
	// or writes this prefix.
	LegacyModeratorKey = collections.NewPrefix(0x01)
	DiscountsKey       = collections.NewPrefix(0x02)
)
