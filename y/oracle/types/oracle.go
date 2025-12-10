package types

import (
	"encoding/binary"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateBasic performs basic validation of the oracle request.
func (r OracleRequest) ValidateBasic() error {
	if r.Category == Category_CATEGORY_UNSPECIFIED {
		return fmt.Errorf("category cannot be unspecified")
	}
	if r.Symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}
	if r.Period == 0 {
		return fmt.Errorf("period must be greater than zero")
	}
	if r.Status == Status_STATUS_UNSPECIFIED {
		return fmt.Errorf("status cannot be unspecified")
	}
	return nil
}

// ValidateBasic performs basic validation of the oracle report.
func (r OracleReport) ValidateBasic() error {
	if r.RequestId == 0 {
		return fmt.Errorf("request id cannot be zero")
	}
	if r.Nonce == 0 {
		return fmt.Errorf("nonce must be greater than zero")
	}
	if r.Provider == "" {
		return fmt.Errorf("provider cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(r.Provider); err != nil {
		return fmt.Errorf("invalid provider address: %w", err)
	}
	if r.RawData == "" {
		return fmt.Errorf("raw data cannot be empty")
	}
	if _, ok := new(big.Float).SetString(r.RawData); !ok {
		return fmt.Errorf("raw data must be a valid decimal")
	}
	if len(r.Signature) == 0 {
		return fmt.Errorf("signature cannot be empty")
	}
	return nil
}

// Bytes returns canonical sign bytes for oracle report verification.
func (r OracleReport) Bytes() ([]byte, error) {
	domain := []byte("guru.oracle.v2.OracleReport")

	addr, err := sdk.AccAddressFromBech32(r.Provider)
	if err != nil {
		return nil, fmt.Errorf("invalid provider bech32: %w", err)
	}

	buf := make([]byte, 0, len(domain)+8+8+4+len(r.RawData)+len(addr))
	buf = append(buf, domain...)

	var u64 [8]byte
	binary.BigEndian.PutUint64(u64[:], r.RequestId)
	buf = append(buf, u64[:]...)
	binary.BigEndian.PutUint64(u64[:], r.Nonce)
	buf = append(buf, u64[:]...)

	var l4 [4]byte
	binary.BigEndian.PutUint32(l4[:], uint32(len(r.RawData)))
	buf = append(buf, l4[:]...)
	buf = append(buf, []byte(r.RawData)...)

	return append(buf, addr.Bytes()...), nil
}
