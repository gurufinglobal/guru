package types

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ParseOracleDecimal parses a chain-acceptable decimal string into a LegacyDec.
//
// We intentionally use sdkmath.LegacyDec (fixed precision, deterministic) rather than
// math/big.Float to avoid consensus-sensitive floating point behavior.
//
// Accepted forms:
// - "123", "-123"
// - "123.456", "-123.456"
// - ".5" (normalized to "0.5")
// - "5." (normalized to "5")
// - optional leading '+' (ignored)
//
// Exponents (e/E/p/P), hex prefixes, underscores, and fractions are rejected.
// Fractional digits beyond sdkmath.LegacyPrecision (18) are truncated (discarded).
func ParseOracleDecimal(raw string) (sdkmath.LegacyDec, error) {
	if raw == "" {
		return sdkmath.LegacyDec{}, fmt.Errorf("raw data is empty")
	}

	sign := ""
	if raw[0] == '+' || raw[0] == '-' {
		sign = string(raw[0])
		raw = raw[1:]
		if raw == "" {
			return sdkmath.LegacyDec{}, fmt.Errorf("raw data is empty")
		}
		if sign == "+" {
			sign = ""
		}
	}

	// Reject bare ".".
	if raw == "." {
		return sdkmath.LegacyDec{}, fmt.Errorf("raw data is not a valid decimal")
	}

	// Normalize forms accepted by big.Float.Parse but rejected by LegacyNewDecFromStr.
	if strings.HasPrefix(raw, ".") {
		raw = "0" + raw
	}
	if strings.HasSuffix(raw, ".") {
		raw = strings.TrimSuffix(raw, ".")
	}

	// Enforce strict decimal format before truncation.
	dotCount := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '.' {
			dotCount++
			if dotCount > 1 {
				return sdkmath.LegacyDec{}, fmt.Errorf("raw data is not a valid decimal")
			}
			if i == 0 || i == len(raw)-1 {
				return sdkmath.LegacyDec{}, fmt.Errorf("raw data is not a valid decimal")
			}
			continue
		}
		if c < '0' || c > '9' {
			return sdkmath.LegacyDec{}, fmt.Errorf("raw data is not a valid decimal")
		}
	}

	// Truncate fractional digits (no rounding) to LegacyDec precision.
	if dot := strings.IndexByte(raw, '.'); dot >= 0 {
		frac := raw[dot+1:]
		if len(frac) > sdkmath.LegacyPrecision {
			raw = raw[:dot+1] + frac[:sdkmath.LegacyPrecision]
		}
	}

	dec, err := sdkmath.LegacyNewDecFromStr(sign + raw)
	if err != nil {
		return sdkmath.LegacyDec{}, err
	}
	return dec, nil
}

// FormatOracleDecimal returns a deterministic, minimal decimal string (no exponent,
// no trailing zeros) for a LegacyDec.
func FormatOracleDecimal(d sdkmath.LegacyDec) string {
	s := d.String()
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// ProtoDefinedCategories returns all categories defined in the proto enum (excluding UNSPECIFIED),
// in deterministic order.
func ProtoDefinedCategories() []Category {
	keys := make([]int, 0, len(Category_name))
	for k := range Category_name {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)

	out := make([]Category, 0, len(keys))
	for _, k := range keys {
		if k == int(Category_CATEGORY_UNSPECIFIED) {
			continue
		}
		out = append(out, Category(k))
	}
	return out
}

// IsKnownCategory reports whether the provided enum value is defined in the proto enum.
// NOTE: proto3 technically allows unknown enum numbers on the wire, so we enforce known-ness explicitly.
func IsKnownCategory(cat Category) bool {
	_, ok := Category_name[int32(cat)]
	return ok
}

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
	if _, err := ParseOracleDecimal(r.RawData); err != nil {
		return fmt.Errorf("raw data must be a valid decimal: %w", err)
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

	rawDataLen := len(r.RawData)
	if rawDataLen > int(^uint32(0)) {
		return nil, fmt.Errorf("raw data too large")
	}
	var l4 [4]byte
	binary.BigEndian.PutUint32(l4[:], uint32(rawDataLen))
	buf = append(buf, l4[:]...)
	buf = append(buf, []byte(r.RawData)...)

	return append(buf, addr.Bytes()...), nil
}
