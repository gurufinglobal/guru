// Package ethsecp256k1 is a compatibility layer that re-exports the
// cosmos/evm ethsecp256k1 types. This avoids duplicate proto registration
// which would cause "unknown message type" errors at runtime.
//
// The cosmos/evm v0.5.1 implementation already includes full EIP-712
// verification support (both current and legacy encodings), so no
// guru-specific overrides are needed.
package ethsecp256k1

import (
	evmcrypto "github.com/cosmos/evm/crypto/ethsecp256k1"
)

// Type aliases - these are the SAME Go types as cosmos/evm's types.
// Using type aliases (=) means there is only one Go type and therefore
// only one proto registration. This eliminates the duplicate registration
// that was causing proto.MessageName() to return "" for one of the types.
type (
	PubKey  = evmcrypto.PubKey
	PrivKey = evmcrypto.PrivKey
)

// Re-export constants
const (
	PrivKeySize = evmcrypto.PrivKeySize
	PubKeySize  = evmcrypto.PubKeySize
	KeyType     = evmcrypto.KeyType
	PrivKeyName = evmcrypto.PrivKeyName
	PubKeyName  = evmcrypto.PubKeyName
)

// GenerateKey re-exports the key generation function.
var GenerateKey = evmcrypto.GenerateKey
