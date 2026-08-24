package types

import "cosmossdk.io/collections"

const (
	ModuleName = "oracle"
	StoreKey   = ModuleName
)

var (
	ParamsKey  = collections.NewPrefix(0x01)
	TasksKey   = collections.NewPrefix(0x02)
	LatestKey  = collections.NewPrefix(0x03)
	HistoryKey = collections.NewPrefix(0x04)

	TaskScheduleKey         = collections.NewPrefix(0x05)
	TaskScheduleBySymbolKey = collections.NewPrefix(0x06)
)
