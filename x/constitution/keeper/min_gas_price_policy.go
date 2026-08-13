package keeper

import (
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	"github.com/gurufinglobal/guru/v2/x/constitution/types"
)

func pendingDelayBlocksForInterval(sourceSubmissionInterval uint32) uint32 {
	delayBlocks := sourceSubmissionInterval
	// The target oracle task's submission_interval is an operational UX rule:
	// validators should keep it stable enough for users to predict the pending
	// fee, but the on-chain apply delay is capped so refreshed data cannot wait
	// indefinitely behind an overly large interval.
	if delayBlocks > types.MinGasPricePendingDelayCap {
		delayBlocks = types.MinGasPricePendingDelayCap
	}
	return delayBlocks
}

func calculateRawMinGasPrice(priceAtoms sdkmath.Int) sdkmath.Int {
	scaleFactor := mustIntFromString(types.MinGasPriceScaleFactor)
	pricePrecision := mustIntFromString(types.MinGasPriceOraclePricePrecision)
	// Oracle prices are stored with 18 decimal places. The scale factor is the
	// min gas price target when the oracle price is 1 USD per chain token.
	return scaleFactor.Mul(pricePrecision).Quo(priceAtoms)
}

func clampMinGasPrice(raw sdkmath.Int, current sdkmath.LegacyDec) sdkmath.LegacyDec {
	rawDec := raw.ToLegacyDec()
	lower := current.MulInt64(int64(types.SeparationRatioScalePPM - types.MinGasPriceClampPPM)).QuoInt64(int64(types.SeparationRatioScalePPM))
	upper := current.MulInt64(int64(types.SeparationRatioScalePPM + types.MinGasPriceClampPPM)).QuoInt64(int64(types.SeparationRatioScalePPM))
	switch {
	case rawDec.LT(lower):
		return lower
	case rawDec.GT(upper):
		return upper
	default:
		return rawDec
	}
}

func parsePositiveDec(value string, fieldName string) (sdkmath.LegacyDec, error) {
	parsed, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(value))
	if err != nil || parsed.IsNil() || !parsed.IsPositive() {
		return sdkmath.LegacyDec{}, types.ErrInvalidMinGasPrice.Wrapf("%s must be a positive decimal string", fieldName)
	}
	return parsed, nil
}

func parseNonNegativeInt(value string, fieldName string) (sdkmath.Int, error) {
	parsed, ok := sdkmath.NewIntFromString(strings.TrimSpace(value))
	if !ok || parsed.IsNegative() {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrapf("%s must be a non-negative integer string", fieldName)
	}
	return parsed, nil
}

func parsePositiveOraclePriceAtoms(value string) (sdkmath.Int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price cannot be empty")
	}
	if strings.HasPrefix(trimmed, "-") {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price must be positive")
	}
	trimmed = strings.TrimPrefix(trimmed, "+")

	parts := strings.Split(trimmed, ".")
	if len(parts) > 2 {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price has too many decimal points")
	}

	integer := parts[0]
	fractional := ""
	if len(parts) == 2 {
		fractional = parts[1]
	}
	if integer == "" && fractional == "" {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price cannot be empty")
	}
	if integer == "" {
		integer = "0"
	}
	if !isDecimalDigits(integer) || !isDecimalDigits(fractional) {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price must contain only decimal digits")
	}
	if len(fractional) > 18 {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price precision cannot exceed 18 decimal places")
	}
	fractional += strings.Repeat("0", 18-len(fractional))

	atoms, ok := sdkmath.NewIntFromString(trimLeadingDecimalZeros(integer + fractional))
	if !ok || !atoms.IsPositive() {
		return sdkmath.Int{}, types.ErrInvalidMinGasPrice.Wrap("oracle price must be positive")
	}
	return atoms, nil
}

func isDecimalDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func trimLeadingDecimalZeros(value string) string {
	trimmed := strings.TrimLeft(value, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

func normalizeMinGasPriceSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func mustIntFromString(value string) sdkmath.Int {
	parsed, ok := sdkmath.NewIntFromString(value)
	if !ok {
		panic(fmt.Sprintf("invalid integer constant %q", value))
	}
	return parsed
}

func isNotFound(err error) bool {
	return errors.Is(err, collections.ErrNotFound)
}
