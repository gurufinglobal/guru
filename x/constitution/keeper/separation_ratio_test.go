package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	appparams "github.com/gurufinglobal/guru/v2/config"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
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

func TestKeeperExecuteSeparation(t *testing.T) {
	f := setupKeeperFixture(t)
	f.bankKeeper.SetModuleBalance(
		authtypes.FeeCollectorName,
		sdk.NewCoins(
			sdk.NewInt64Coin(appparams.BaseDenom, 11),
			sdk.NewInt64Coin("ufoo", 7),
		),
	)

	require.NoError(t, f.keeper.ExecuteSeparation(f.ctx))

	// 20% goes to base address, 30% is burned, and the remainder stays in fee collector.
	require.Equal(
		t,
		sdk.NewCoins(
			sdk.NewInt64Coin(appparams.BaseDenom, 6),
			sdk.NewInt64Coin("ufoo", 4),
		),
		f.bankKeeper.GetModuleBalance(authtypes.FeeCollectorName),
	)
	require.True(t, f.bankKeeper.GetModuleBalance(constitutiontypes.ModuleName).Empty())

	baseAddressBytes, err := f.keeper.accountCodec.StringToBytes(f.baseAddress)
	require.NoError(t, err)
	require.Equal(
		t,
		sdk.NewCoins(
			sdk.NewInt64Coin(appparams.BaseDenom, 2),
			sdk.NewInt64Coin("ufoo", 1),
		),
		f.bankKeeper.GetAccountBalance(sdk.AccAddress(baseAddressBytes)),
	)
}

func TestKeeperExecuteSeparationExtremeRatios(t *testing.T) {
	tests := []struct {
		name                     string
		ratio                    *testRatio
		feeCollectorBalance      sdk.Coins
		expectedFeeCollector     sdk.Coins
		expectedBaseAccount      sdk.Coins
		expectedConstitutionPool sdk.Coins
	}{
		{
			name: "all base",
			ratio: &testRatio{
				base:       1_000_000,
				burn:       0,
				validators: 0,
			},
			feeCollectorBalance: sdk.NewCoins(
				sdk.NewInt64Coin(appparams.BaseDenom, 13),
				sdk.NewInt64Coin("ufoo", 8),
			),
			expectedFeeCollector: sdk.NewCoins(),
			expectedBaseAccount: sdk.NewCoins(
				sdk.NewInt64Coin(appparams.BaseDenom, 13),
				sdk.NewInt64Coin("ufoo", 8),
			),
			expectedConstitutionPool: sdk.NewCoins(),
		},
		{
			name: "all burn",
			ratio: &testRatio{
				base:       0,
				burn:       1_000_000,
				validators: 0,
			},
			feeCollectorBalance: sdk.NewCoins(
				sdk.NewInt64Coin(appparams.BaseDenom, 13),
				sdk.NewInt64Coin("ufoo", 8),
			),
			expectedFeeCollector:     sdk.NewCoins(),
			expectedBaseAccount:      sdk.NewCoins(),
			expectedConstitutionPool: sdk.NewCoins(),
		},
		{
			name: "all validators",
			ratio: &testRatio{
				base:       0,
				burn:       0,
				validators: 1_000_000,
			},
			feeCollectorBalance: sdk.NewCoins(
				sdk.NewInt64Coin(appparams.BaseDenom, 13),
				sdk.NewInt64Coin("ufoo", 8),
			),
			expectedFeeCollector: sdk.NewCoins(
				sdk.NewInt64Coin(appparams.BaseDenom, 13),
				sdk.NewInt64Coin("ufoo", 8),
			),
			expectedBaseAccount:      sdk.NewCoins(),
			expectedConstitutionPool: sdk.NewCoins(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.SetSeparationRatio(f.ctx, testSeparationRatio(tc.ratio.base, tc.ratio.burn, tc.ratio.validators)))
			f.bankKeeper.SetModuleBalance(authtypes.FeeCollectorName, tc.feeCollectorBalance)

			require.NoError(t, f.keeper.ExecuteSeparation(f.ctx))

			requireCoinsEqual(t, tc.expectedFeeCollector, f.bankKeeper.GetModuleBalance(authtypes.FeeCollectorName))
			requireCoinsEqual(t, tc.expectedConstitutionPool, f.bankKeeper.GetModuleBalance(constitutiontypes.ModuleName))

			baseAddressBytes, err := f.keeper.accountCodec.StringToBytes(f.baseAddress)
			require.NoError(t, err)
			requireCoinsEqual(t, tc.expectedBaseAccount, f.bankKeeper.GetAccountBalance(sdk.AccAddress(baseAddressBytes)))
		})
	}
}

func TestKeeperExecuteSeparationNoFeeCollectorBalance(t *testing.T) {
	f := setupKeeperFixture(t)

	require.NoError(t, f.keeper.ExecuteSeparation(f.ctx))

	baseAddressBytes, err := f.keeper.accountCodec.StringToBytes(f.baseAddress)
	require.NoError(t, err)
	require.True(t, f.bankKeeper.GetAccountBalance(sdk.AccAddress(baseAddressBytes)).Empty())
}

func TestKeeperExecuteSeparationFastPathForZeroBaseAndBurn(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.SetSeparationRatio(f.ctx, testSeparationRatio(0, 0, 1_000_000)))
	require.NoError(t, f.keeper.baseAddress.Set(f.ctx, "invalid-base-address"))

	require.NoError(t, f.keeper.ExecuteSeparation(f.ctx))
}

func TestKeeperExecuteSeparationValidation(t *testing.T) {
	t.Run("fails when bank keeper is not configured", func(t *testing.T) {
		f := setupKeeperFixture(t)
		f.keeper.bankKeeper = nil

		require.Error(t, f.keeper.ExecuteSeparation(f.ctx))
	})

	t.Run("fails when stored base address is invalid", func(t *testing.T) {
		f := setupKeeperFixture(t)
		f.bankKeeper.SetModuleBalance(
			authtypes.FeeCollectorName,
			sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 10)),
		)
		require.NoError(t, f.keeper.baseAddress.Set(f.ctx, "invalid-base-address"))

		require.Error(t, f.keeper.ExecuteSeparation(f.ctx))
	})
}

type testRatio struct {
	base       uint32
	burn       uint32
	validators uint32
}

func requireCoinsEqual(t *testing.T, expected, actual sdk.Coins) {
	t.Helper()

	if expected.Empty() {
		require.True(t, actual.Empty())
		return
	}

	require.Equal(t, expected, actual)
}
