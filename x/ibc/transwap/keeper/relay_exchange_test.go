package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	bextypes "github.com/gurufinglobal/guru/v2/x/bex/types"
)

func TestValidateExchangeOutputLimit(t *testing.T) {
	t.Run("nil limit is unlimited", func(t *testing.T) {
		err := validateExchangeOutputLimit(sdkmath.LegacyDec{}, dec("1"), dec("100"))
		require.NoError(t, err)
	})

	t.Run("zero limit is unlimited", func(t *testing.T) {
		err := validateExchangeOutputLimit(sdkmath.LegacyZeroDec(), dec("1"), dec("100"))
		require.NoError(t, err)
	})

	t.Run("equal output amount is allowed", func(t *testing.T) {
		// output limit = 100 * 2 = 200
		err := validateExchangeOutputLimit(dec("100"), dec("2"), dec("200"))
		require.NoError(t, err)
	})

	t.Run("swap amount above output limit is rejected", func(t *testing.T) {
		// output limit = 100 * 2 = 200
		err := validateExchangeOutputLimit(dec("100"), dec("2"), dec("200.000000000000000001"))
		require.Error(t, err)
		require.ErrorContains(t, err, bextypes.ErrExchangeLimitExceeded.Error())
	})

	t.Run("fractional rate direction is handled", func(t *testing.T) {
		// output limit = 100 * 0.5 = 50
		require.NoError(t, validateExchangeOutputLimit(dec("100"), dec("0.5"), dec("50")))

		err := validateExchangeOutputLimit(dec("100"), dec("0.5"), dec("50.000000000000000001"))
		require.Error(t, err)
		require.ErrorContains(t, err, bextypes.ErrExchangeLimitExceeded.Error())
	})
}

func dec(v string) sdkmath.LegacyDec {
	return sdkmath.LegacyMustNewDecFromStr(v)
}
