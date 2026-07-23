// Package uint256 provides a single canonical decimal parser for values that
// cross module and wire-format boundaries.
package uint256

import (
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
)

// MaxDecimalString is the canonical base-10 representation of 2^256 - 1.
const MaxDecimalString = "115792089237316195423570985008687907853269984665640564039457584007913129639935"

var maxValue = func() *big.Int {
	value, ok := new(big.Int).SetString(MaxDecimalString, 10)
	if !ok {
		panic("invalid uint256 maximum")
	}
	return value
}()

// ParseCanonical parses an unsigned uint256 encoded as canonical ASCII base-10.
// Zero is accepted, but signs, whitespace, prefixes, separators, and leading
// zeroes are rejected so every consumer observes exactly the same value.
func ParseCanonical(raw string) (sdkmath.Int, error) {
	if raw == "" {
		return sdkmath.Int{}, fmt.Errorf("value is required")
	}
	if len(raw) > len(MaxDecimalString) {
		return sdkmath.Int{}, fmt.Errorf("value exceeds uint256 maximum")
	}
	if len(raw) > 1 && raw[0] == '0' {
		return sdkmath.Int{}, fmt.Errorf("value must be canonical decimal without leading zeroes")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return sdkmath.Int{}, fmt.Errorf("value must contain only ASCII decimal digits")
		}
	}
	if len(raw) == len(MaxDecimalString) && raw > MaxDecimalString {
		return sdkmath.Int{}, fmt.Errorf("value exceeds uint256 maximum")
	}

	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return sdkmath.Int{}, fmt.Errorf("value is not a decimal integer")
	}
	return sdkmath.NewIntFromBigInt(value), nil
}

// ParseCanonicalPositive is ParseCanonical with the additional requirement
// that the value is strictly positive.
func ParseCanonicalPositive(raw string) (sdkmath.Int, error) {
	value, err := ParseCanonical(raw)
	if err != nil {
		return sdkmath.Int{}, err
	}
	if !value.IsPositive() {
		return sdkmath.Int{}, fmt.Errorf("value must be strictly positive")
	}
	return value, nil
}

// Max returns 2^256 - 1 as a fresh math.Int value.
func Max() sdkmath.Int {
	return sdkmath.NewIntFromBigInt(new(big.Int).Set(maxValue))
}
