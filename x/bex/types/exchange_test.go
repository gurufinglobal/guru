package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"

	"github.com/stretchr/testify/require"
)

func TestValidateExchangeLimit(t *testing.T) {
	t.Run("nil limit returns error", func(t *testing.T) {
		err := ValidateExchangeLimit(sdkmath.LegacyDec{})
		require.Error(t, err)
		require.ErrorContains(t, err, ErrInvalidLimit.Error())
	})

	t.Run("negative limit returns error", func(t *testing.T) {
		err := ValidateExchangeLimit(sdkmath.LegacyMustNewDecFromStr("-1"))
		require.Error(t, err)
		require.ErrorContains(t, err, ErrInvalidLimit.Error())
	})

	t.Run("zero limit is valid", func(t *testing.T) {
		err := ValidateExchangeLimit(sdkmath.LegacyZeroDec())
		require.NoError(t, err)
	})

	t.Run("positive limit is valid", func(t *testing.T) {
		err := ValidateExchangeLimit(sdkmath.LegacyMustNewDecFromStr("123.45"))
		require.NoError(t, err)
	})
}
