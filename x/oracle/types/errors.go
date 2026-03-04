package types

import (
	errorsmod "cosmossdk.io/errors"
)

const (
	codeInvalidRequest = uint32(iota) + 2
	codeInvalidNonce
	codeInvalidProvider
	codeInvalidRawData
	codeRequestNotFound
	codeReportExists
	codeModuleDisabled
)

var (
	// ErrInvalidRequest is returned when the request is invalid.
	ErrInvalidRequest = errorsmod.Register(ModuleName, codeInvalidRequest, "invalid request")
	// ErrInvalidNonce is returned when the nonce is invalid.
	ErrInvalidNonce = errorsmod.Register(ModuleName, codeInvalidNonce, "invalid nonce")
	// ErrInvalidProvider is returned when the provider is invalid.
	ErrInvalidProvider = errorsmod.Register(ModuleName, codeInvalidProvider, "invalid provider")
	// ErrInvalidRawData is returned when the raw data is invalid.
	ErrInvalidRawData = errorsmod.Register(ModuleName, codeInvalidRawData, "invalid raw data")
	// ErrRequestNotFound is returned when the oracle request is not found.
	ErrRequestNotFound = errorsmod.Register(ModuleName, codeRequestNotFound, "oracle request not found")
	// ErrReportExists is returned when the oracle report already exists.
	ErrReportExists = errorsmod.Register(ModuleName, codeReportExists, "oracle report already exists")
	// ErrModuleDisabled is returned when the oracle module is disabled.
	ErrModuleDisabled = errorsmod.Register(ModuleName, codeModuleDisabled, "oracle module is disabled")
)
