package types

import "cosmossdk.io/collections"

const (
	ModuleName = "constitution"
	StoreKey   = ModuleName
)

// Keys of the KVStore
var (
	ParamsKey = collections.NewPrefix(0x01)
)
