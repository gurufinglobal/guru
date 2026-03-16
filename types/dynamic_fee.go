package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	antetypes "github.com/cosmos/evm/ante/types"
)

// HasDynamicFeeExtensionOption returns true if the tx implements the `ExtensionOptionDynamicFeeTx` extension option.
func HasDynamicFeeExtensionOption(anyType *codectypes.Any) bool {
	_, ok := anyType.GetCachedValue().(*antetypes.ExtensionOptionDynamicFeeTx)
	return ok
}
