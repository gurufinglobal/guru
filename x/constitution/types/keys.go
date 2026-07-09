package types

import "cosmossdk.io/collections"

const (
	ModuleName = "constitution"
	StoreKey   = ModuleName
)

// Keys of the KVStore
var (
	ParamsKey           = collections.NewPrefix(0x01)
	BaseAddressKey      = collections.NewPrefix(0x02)
	ModeratorAddressKey = collections.NewPrefix(0x03)
	SeparationRatioKey  = collections.NewPrefix(0x04)
	MinGasPriceKey      = collections.NewPrefix(0x05)
)
