package types

import "cosmossdk.io/errors"

// x/constitution module sentinel errors.
var (
	ErrInvalidAuthority = errors.Register(ModuleName, 2, "invalid authority")
	ErrInvalidRequest   = errors.Register(ModuleName, 3, "invalid request")
	ErrInvalidParams    = errors.Register(ModuleName, 4, "invalid params")
	ErrSelfBondBelowMin = errors.Register(ModuleName, 5, "self-bond below minimum")

	ErrReadGenesisField        = errors.Register(ModuleName, 6, "failed to read genesis field")
	ErrDecodeGenesisField      = errors.Register(ModuleName, 7, "failed to decode genesis field")
	ErrRequiredGenesisField    = errors.Register(ModuleName, 8, "required genesis field is missing")
	ErrOpenGenesisTargetField  = errors.Register(ModuleName, 9, "failed to open genesis target field")
	ErrNilGenesisTargetWriter  = errors.Register(ModuleName, 10, "genesis target field writer is nil")
	ErrEncodeGenesisField      = errors.Register(ModuleName, 11, "failed to encode genesis field")
	ErrCloseGenesisFieldWriter = errors.Register(ModuleName, 12, "failed to close genesis field writer")
)
