package types

import (
	errorsmod "cosmossdk.io/errors"
)

// errors
var (
	ErrInvalidDiscount = errorsmod.Register(ModuleName, 2, "invalid discount")
	ErrWrongModerator  = errorsmod.Register(ModuleName, 3, "the operation is allowed only from moderator address")
	ErrInvalidJSONFile = errorsmod.Register(ModuleName, 4, "invalid json file")
)
