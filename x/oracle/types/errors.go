package types

import "cosmossdk.io/errors"

var (
	ErrInvalidAuthority = errors.Register(ModuleName, 2, "invalid authority")
	ErrInvalidRequest   = errors.Register(ModuleName, 3, "invalid request")
	ErrInvalidParams    = errors.Register(ModuleName, 4, "invalid params")
	ErrInvalidTask      = errors.Register(ModuleName, 5, "invalid oracle task")
	ErrInvalidValue     = errors.Register(ModuleName, 6, "invalid oracle value")

	ErrReadGenesisField        = errors.Register(ModuleName, 7, "failed to read genesis field")
	ErrDecodeGenesisField      = errors.Register(ModuleName, 8, "failed to decode genesis field")
	ErrOpenGenesisTargetField  = errors.Register(ModuleName, 9, "failed to open genesis target field")
	ErrNilGenesisTargetWriter  = errors.Register(ModuleName, 10, "genesis target field writer is nil")
	ErrEncodeGenesisField      = errors.Register(ModuleName, 11, "failed to encode genesis field")
	ErrCloseGenesisFieldWriter = errors.Register(ModuleName, 12, "failed to close genesis field writer")
)
