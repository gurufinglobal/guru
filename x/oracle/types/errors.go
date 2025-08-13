package types

import (
	errorsmod "cosmossdk.io/errors"
)

const (
	codeInvalidRequestID = uint32(iota) + 2
	codeInvalidNonce
	codeInvalidProvider
	codeInvalidRawData
	codeQuorumNotMet
)

var (
	ErrInvalidRequestId = errorsmod.Register(ModuleName, codeInvalidRequestID, "invalid request id")
	ErrInvalidNonce     = errorsmod.Register(ModuleName, codeInvalidNonce, "invalid nonce")
	ErrInvalidProvider  = errorsmod.Register(ModuleName, codeInvalidProvider, "invalid provider")
	ErrInvalidRawData   = errorsmod.Register(ModuleName, codeInvalidRawData, "invalid raw data")
	ErrQuorumNotMet     = errorsmod.Register(ModuleName, codeQuorumNotMet, "quorum not met")
)
