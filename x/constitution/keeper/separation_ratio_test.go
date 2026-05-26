package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSeparationRatio(t *testing.T) {
	tests := []struct {
		name      string
		ratio     *testRatio
		shouldErr bool
	}{
		{
			name:      "fails on nil ratio",
			ratio:     nil,
			shouldErr: true,
		},
		{
			name: "fails when ratio part exceeds scale",
			ratio: &testRatio{
				base:       1_000_001,
				burn:       0,
				validators: 0,
			},
			shouldErr: true,
		},
		{
			name: "fails when sum is not one million",
			ratio: &testRatio{
				base:       200_000,
				burn:       300_000,
				validators: 400_000,
			},
			shouldErr: true,
		},
		{
			name: "passes on valid ratio",
			ratio: &testRatio{
				base:       200_000,
				burn:       300_000,
				validators: 500_000,
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ratio = (*testRatio)(nil)
			if tc.ratio != nil {
				ratio = &testRatio{
					base:       tc.ratio.base,
					burn:       tc.ratio.burn,
					validators: tc.ratio.validators,
				}
			}

			var err error
			if ratio == nil {
				err = ValidateSeparationRatio(nil)
			} else {
				err = ValidateSeparationRatio(testSeparationRatio(ratio.base, ratio.burn, ratio.validators))
			}

			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKeeperSeparationRatioGetSetUpdate(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	ratio := testSeparationRatio(100_000, 200_000, 700_000)
	require.NoError(t, f.keeper.SetSeparationRatio(f.ctx, ratio))

	gotRatio, err := f.keeper.GetSeparationRatio(f.ctx)
	require.NoError(t, err)
	require.Equal(t, ratio.GetBasePpm(), gotRatio.GetBasePpm())
	require.Equal(t, ratio.GetBurnPpm(), gotRatio.GetBurnPpm())
	require.Equal(t, ratio.GetValidatorsPpm(), gotRatio.GetValidatorsPpm())

	updatedRatio := testSeparationRatio(250_000, 250_000, 500_000)
	require.NoError(t, f.keeper.UpdateSeparationRatio(f.ctx, updatedRatio))

	gotRatio, err = f.keeper.GetSeparationRatio(f.ctx)
	require.NoError(t, err)
	require.Equal(t, updatedRatio.GetBasePpm(), gotRatio.GetBasePpm())
	require.Equal(t, updatedRatio.GetBurnPpm(), gotRatio.GetBurnPpm())
	require.Equal(t, updatedRatio.GetValidatorsPpm(), gotRatio.GetValidatorsPpm())
}

func TestKeeperSeparationRatioValidation(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	require.Error(t, f.keeper.SetSeparationRatio(f.ctx, nil))
	require.Error(t, f.keeper.SetSeparationRatio(f.ctx, testSeparationRatio(100_000, 200_000, 800_000)))
	require.Error(t, f.keeper.SetSeparationRatio(f.ctx, testSeparationRatio(1_000_001, 0, 0)))
}

type testRatio struct {
	base       uint32
	burn       uint32
	validators uint32
}
