package types

import (
	errorsmod "cosmossdk.io/errors"
)

// errors
var (
	ErrWrongModerator           = errorsmod.Register(ModuleName, 2, "the operation is allowed only from moderator address")
	ErrWrongAdmin               = errorsmod.Register(ModuleName, 3, "the operation is allowed only from admin address")
	ErrInvalidExchange          = errorsmod.Register(ModuleName, 4, "invalid exchange")
	ErrInvalidID                = errorsmod.Register(ModuleName, 5, "invalid id")
	ErrInvalidDenom             = errorsmod.Register(ModuleName, 6, "invalid denom")
	ErrInvalidPort              = errorsmod.Register(ModuleName, 7, "invalid port")
	ErrInvalidChannel           = errorsmod.Register(ModuleName, 8, "invalid channel")
	ErrInvalidRate              = errorsmod.Register(ModuleName, 9, "invalid rate")
	ErrInvalidFee               = errorsmod.Register(ModuleName, 10, "invalid fee")
	ErrInvalidLimit             = errorsmod.Register(ModuleName, 11, "invalid limit")
	ErrInvalidStatus            = errorsmod.Register(ModuleName, 12, "invalid status")
	ErrInvalidMetadata          = errorsmod.Register(ModuleName, 13, "invalid metadata")
	ErrInvalidKey               = errorsmod.Register(ModuleName, 14, "invalid key")
	ErrInvalidValue             = errorsmod.Register(ModuleName, 15, "invalid value")
	ErrInvalidRatemeter         = errorsmod.Register(ModuleName, 16, "invalid ratemeter")
	ErrInvalidRequestCountLimit = errorsmod.Register(ModuleName, 17, "invalid request count limit")
	ErrInvalidRequestPeriod     = errorsmod.Register(ModuleName, 18, "invalid request period")
	ErrInvalidJSONFile          = errorsmod.Register(ModuleName, 19, "invalid json file")
	ErrInsufficientBalance      = errorsmod.Register(ModuleName, 20, "unsufficient balance")
)
