package types

import (
	"encoding/json"
)

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
