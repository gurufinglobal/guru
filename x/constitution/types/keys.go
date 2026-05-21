package types

import "cosmossdk.io/collections"

const (
	ModuleName = "constitution"
	StoreKey   = ModuleName
)

var (
	ParamsKey            = collections.NewPrefix(0x01)
	ChangedValidatorsKey = collections.NewPrefix(0x02)
	EnforceAllBondedKey  = collections.NewPrefix(0x03)
)
