package types

import "cosmossdk.io/collections"

const (
	ModuleName = "bex"
	StoreKey   = ModuleName
)

var (
	AdminsKey            = collections.NewPrefix(0x01)
	ExchangesKey         = collections.NewPrefix(0x02)
	ExchangesByAdminKey  = collections.NewPrefix(0x03)
	ReserveByAddressKey  = collections.NewPrefix(0x04)
	NextExchangeIDKey    = collections.NewPrefix(0x05)
	CollectedFeesKey     = collections.NewPrefix(0x06)
	LockedFeesKey        = collections.NewPrefix(0x07)
	VolumeWindowKey      = collections.NewPrefix(0x08)
	VolumePruneCursorKey = collections.NewPrefix(0x09)
)
