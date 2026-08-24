package domain

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	sdkmath "cosmossdk.io/math"
)

var (
	errInvalidNumber = errors.New("invalid numeric value")
	two              = big.NewInt(2)
)

// NormalizeDecimal accepts plain or scientific decimal syntax and returns the
// exact canonical fixed-18 representation used by Guru. It never rounds.
func NormalizeDecimal(input string) (string, error) {
	if len(input) == 0 || len(input) > MaxNumericToken {
		return "", fmt.Errorf("%w: token length", errInvalidNumber)
	}

	sign := ""
	pos := 0
	if input[0] == '+' || input[0] == '-' {
		if input[0] == '-' {
			sign = "-"
		}
		pos++
		if pos == len(input) {
			return "", errInvalidNumber
		}
	}

	exponentIndex := -1
	dotIndex := -1
	for i := pos; i < len(input); i++ {
		switch input[i] {
		case '.':
			if dotIndex >= 0 || exponentIndex >= 0 {
				return "", errInvalidNumber
			}
			dotIndex = i
		case 'e', 'E':
			if exponentIndex >= 0 {
				return "", errInvalidNumber
			}
			exponentIndex = i
		default:
			if input[i] < '0' || input[i] > '9' {
				if exponentIndex >= 0 && i == exponentIndex+1 && (input[i] == '+' || input[i] == '-') {
					continue
				}
				return "", errInvalidNumber
			}
		}
	}

	mantissaEnd := len(input)
	exponent := 0
	if exponentIndex >= 0 {
		mantissaEnd = exponentIndex
		if exponentIndex == pos || exponentIndex+1 == len(input) {
			return "", errInvalidNumber
		}
		expText := input[exponentIndex+1:]
		if expText == "+" || expText == "-" {
			return "", errInvalidNumber
		}
		parsed, err := strconv.Atoi(expText)
		if err != nil || parsed < -MaxAbsoluteExponent || parsed > MaxAbsoluteExponent {
			return "", fmt.Errorf("%w: exponent", errInvalidNumber)
		}
		exponent = parsed
	}

	mantissa := input[pos:mantissaEnd]
	if mantissa == "" || mantissa == "." {
		return "", errInvalidNumber
	}
	decimalDigits := 0
	if dotIndex >= 0 {
		localDot := dotIndex - pos
		decimalDigits = len(mantissa) - localDot - 1
		mantissa = mantissa[:localDot] + mantissa[localDot+1:]
	}
	if mantissa == "" {
		return "", errInvalidNumber
	}
	for _, digit := range mantissa {
		if digit < '0' || digit > '9' {
			return "", errInvalidNumber
		}
	}
	if len(mantissa) > MaxCoefficientDigits {
		return "", fmt.Errorf("%w: coefficient", errInvalidNumber)
	}

	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		return sdkmath.LegacyZeroDec().String(), nil
	}

	scale := decimalDigits - exponent
	for scale > 0 && strings.HasSuffix(mantissa, "0") {
		mantissa = strings.TrimSuffix(mantissa, "0")
		scale--
	}
	if scale > sdkmath.LegacyPrecision {
		return "", fmt.Errorf("%w: more than 18 fractional digits", errInvalidNumber)
	}

	var plain string
	switch {
	case scale <= 0:
		plain = sign + mantissa + strings.Repeat("0", -scale)
	case scale >= len(mantissa):
		plain = sign + "0." + strings.Repeat("0", scale-len(mantissa)) + mantissa
	default:
		split := len(mantissa) - scale
		plain = sign + mantissa[:split] + "." + mantissa[split:]
	}
	dec, err := sdkmath.LegacyNewDecFromStr(plain)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errInvalidNumber, err)
	}
	return dec.String(), nil
}

func ParseCanonicalDecimal(value string) (sdkmath.LegacyDec, error) {
	if len(value) == 0 || len(value) > MaxValueBytes {
		return sdkmath.LegacyDec{}, errors.New("decimal length is invalid")
	}
	dec, err := sdkmath.LegacyNewDecFromStr(value)
	if err != nil {
		return sdkmath.LegacyDec{}, err
	}
	if dec.String() != value {
		return sdkmath.LegacyDec{}, errors.New("decimal is not canonical")
	}
	return dec, nil
}

func Median(values []sdkmath.LegacyDec) (sdkmath.LegacyDec, error) {
	if len(values) == 0 {
		return sdkmath.LegacyDec{}, errors.New("median requires at least one value")
	}
	sorted := make([]sdkmath.LegacyDec, len(values))
	for i := range values {
		if values[i].IsNil() {
			return sdkmath.LegacyDec{}, errors.New("median contains nil value")
		}
		sorted[i] = values[i].Clone()
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].LT(sorted[j]) })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid], nil
	}
	sum := new(big.Int).Add(sorted[mid-1].BigInt(), sorted[mid].BigInt())
	quotient := new(big.Int).Quo(sum, two)
	result := sdkmath.LegacyNewDecFromBigIntWithPrec(quotient, sdkmath.LegacyPrecision)
	if _, err := sdkmath.LegacyNewDecFromStr(result.String()); err != nil {
		return sdkmath.LegacyDec{}, fmt.Errorf("median result is out of range: %w", err)
	}
	return result, nil
}
