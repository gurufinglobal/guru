package types

import "cosmossdk.io/errors"

// x/constitution module sentinel errors.
var (
	ErrInvalidAuthority = errors.Register(ModuleName, 2, "invalid authority")
	ErrInvalidRequest   = errors.Register(ModuleName, 3, "invalid request")
	ErrInvalidParams    = errors.Register(ModuleName, 4, "invalid params")
	ErrSelfBondBelowMin = errors.Register(ModuleName, 5, "self-bond below minimum")
)
