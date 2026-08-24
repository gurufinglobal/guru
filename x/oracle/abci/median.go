package abci

import (
	"math/big"
	"sort"

	sdkmath "cosmossdk.io/math"
)

// median uses unbounded integer atoms for the even midpoint so two valid
// LegacyDec values cannot overflow before division.
func median(values []sdkmath.LegacyDec) sdkmath.LegacyDec {
	sort.Slice(values, func(i, j int) bool {
		return values[i].LT(values[j])
	})

	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}

	sum := new(big.Int).Add(values[mid-1].BigInt(), values[mid].BigInt())
	quotient := new(big.Int).Quo(sum, big.NewInt(2))

	return sdkmath.LegacyNewDecFromBigIntWithPrec(quotient, sdkmath.LegacyPrecision)
}
