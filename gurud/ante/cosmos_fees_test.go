package ante

import (
	"testing"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	feepolicytypes "github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

func TestApplyDiscountToBaseFee(t *testing.T) {
	baseFee := sdk.NewCoins(
		sdk.NewCoin("aguru", math.NewInt(100)),
		sdk.NewCoin("aguru2", math.NewInt(80)),
	)

	t.Run("percent discount", func(t *testing.T) {
		discounted := applyDiscountToBaseFee(baseFee, feepolicytypes.Discount{
			DiscountType: feepolicytypes.FeeDiscountTypePercent,
			Amount:       math.LegacyNewDec(25),
		})

		require.Equal(t, math.NewInt(75), discounted.AmountOf("aguru"))
		require.Equal(t, math.NewInt(60), discounted.AmountOf("aguru2"))
	})

	t.Run("percent over 100 is clamped", func(t *testing.T) {
		discounted := applyDiscountToBaseFee(baseFee, feepolicytypes.Discount{
			DiscountType: feepolicytypes.FeeDiscountTypePercent,
			Amount:       math.LegacyNewDec(150),
		})

		require.True(t, discounted.IsZero())
	})

	t.Run("fixed discount is capped by original fee", func(t *testing.T) {
		discounted := applyDiscountToBaseFee(baseFee, feepolicytypes.Discount{
			DiscountType: feepolicytypes.FeeDiscountTypeFixed,
			Amount:       math.LegacyNewDec(90),
		})

		require.Equal(t, math.NewInt(90), discounted.AmountOf("aguru"))
		require.Equal(t, math.NewInt(80), discounted.AmountOf("aguru2"))
	})
}
