package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v2/config"
	constitutionv1 "github.com/gurufinglobal/guru/v2/x/constitution/types"
	"github.com/stretchr/testify/require"
)

func TestValidateMinValidatorBondAmount(t *testing.T) {
	tests := []struct {
		name      string
		coin      *sdk.Coin
		shouldErr bool
	}{
		{"fails on nil coin", nil, true},
		{"fails on invalid denom", &sdk.Coin{Denom: "1invalid", Amount: sdkmath.NewInt(10)}, true},
		{"fails on non-base denom", &sdk.Coin{Denom: "uatom", Amount: sdkmath.NewInt(10)}, true},
		{"fails on uninitialized amount", &sdk.Coin{Denom: appparams.BaseDenom}, true},
		{"fails on zero amount", &sdk.Coin{Denom: appparams.BaseDenom, Amount: sdkmath.ZeroInt()}, true},
		{"fails on negative amount", &sdk.Coin{Denom: appparams.BaseDenom, Amount: sdkmath.NewInt(-1)}, true},
		{"passes on positive integer amount", &sdk.Coin{Denom: appparams.BaseDenom, Amount: sdkmath.NewInt(11)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateMinValidatorBondAmount(tc.coin)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateParamsAndMinValidatorBondAmount(t *testing.T) {
	require.Error(t, ValidateParams(nil))
	_, err := MinValidatorBondAmount(nil)
	require.Error(t, err)

	params := testParams("10")
	require.NoError(t, ValidateParams(params))
	_, err = MinValidatorBondAmount(params)
	require.NoError(t, err)
}

func TestParamsGetSetAndMinBondGetters(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	require.NoError(t, f.keeper.SetParams(f.ctx, testParams("13")))

	params, err := f.keeper.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "13", params.GetMinValidatorBondAmount().Amount.String())

	coin, err := f.keeper.GetMinValidatorBondAmountCoin(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "13", coin.Amount.String())

	amount, err := f.keeper.GetMinValidatorBondAmount(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "13", amount.String())
}

func TestGetMinValidatorBondAmountFailsOnCorruptedStoredParams(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)
	require.NoError(t, f.keeper.params.Set(f.ctx, constitutionv1.Params{
		MinValidatorBondAmount: &sdk.Coin{
			Denom:  "uatom",
			Amount: sdkmath.NewInt(10),
		},
	}))

	_, err := f.keeper.GetMinValidatorBondAmount(f.ctx)
	require.Error(t, err)
}

func TestUpdateParams(t *testing.T) {
	tests := []struct {
		name      string
		oldAmount string
		newAmount string
	}{
		{"updates when increased", "10", "20"},
		{"updates when unchanged", "10", "10"},
		{"updates when decreased", "20", "10"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.SetParams(f.ctx, testParams(tc.oldAmount)))
			require.NoError(t, f.keeper.UpdateParams(f.ctx, testParams(tc.newAmount)))

			amount, err := f.keeper.GetMinValidatorBondAmount(f.ctx)
			require.NoError(t, err)
			require.Equal(t, tc.newAmount, amount.String())
		})
	}
}

func TestUpdateParamsWithoutExistingParams(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	err := f.keeper.UpdateParams(f.ctx, testParams("11"))
	require.NoError(t, err)

	amount, err := f.keeper.GetMinValidatorBondAmount(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "11", amount.String())
}

func TestSetMinValidatorBondAmount(t *testing.T) {
	tests := []struct {
		name           string
		minBond        *sdk.Coin
		expectErr      bool
		expectedAmount string
	}{
		{
			name: "updates and sets full scan flag on increase",
			minBond: &sdk.Coin{
				Denom:  appparams.BaseDenom,
				Amount: sdkmath.NewInt(15),
			},
			expectedAmount: "15",
		},
		{
			name: "fails on invalid denom",
			minBond: &sdk.Coin{
				Denom:  "uatom",
				Amount: sdkmath.NewInt(15),
			},
			expectErr:      true,
			expectedAmount: "10",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			err := f.keeper.SetMinValidatorBondAmount(f.ctx, tc.minBond)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			amount, err := f.keeper.GetMinValidatorBondAmount(f.ctx)
			require.NoError(t, err)
			require.Equal(t, tc.expectedAmount, amount.String())
		})
	}
}
