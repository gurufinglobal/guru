package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKeyPrefix is the unique prefix for Params in this module store.
	ParamsKeyPrefix = collections.NewPrefix(0)

	// ModeratorAddressKeyPrefix is the unique prefix for moderator address in this module store.
	ModeratorAddressKeyPrefix = collections.NewPrefix(1)
)
