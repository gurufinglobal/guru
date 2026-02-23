package types

import (
	"encoding/json"
)

// EvmCoinInfo struct holds the name and decimals of the EVM denom. The EVM denom
// is the token used to pay fees in the EVM.
type EvmCoinInfo struct {
	Denom         string `json:"denom"`
	ExtendedDenom string `json:"extended_denom"`
	DisplayDenom  string `json:"display_denom"`
	Decimals      uint32 `json:"decimals"`
}

// MarshalEvmCoinInfo marshals EvmCoinInfo to bytes for KV store storage.
func MarshalEvmCoinInfo(info EvmCoinInfo) ([]byte, error) {
	return json.Marshal(info)
}

// UnmarshalEvmCoinInfo unmarshals EvmCoinInfo from bytes.
func UnmarshalEvmCoinInfo(bz []byte) (EvmCoinInfo, error) {
	var info EvmCoinInfo
	err := json.Unmarshal(bz, &info)
	return info, err
}
