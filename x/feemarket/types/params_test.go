package types

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"cosmossdk.io/math"
)

type ParamsTestSuite struct {
	suite.Suite
}

func TestParamsTestSuite(t *testing.T) {
	suite.Run(t, new(ParamsTestSuite))
}

func (suite *ParamsTestSuite) TestParamsValidate() {
	testCases := []struct {
		name     string
		params   Params
		expError bool
	}{
		{"default", DefaultParams(), false},
		{
			"valid",
			NewParams(true, 7, 3, math.LegacyNewDec(2000000000), int64(544435345345435345), math.LegacyNewDecWithPrec(20, 4), DefaultMinGasMultiplier, DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			false,
		},
		{
			"empty",
			Params{},
			true,
		},
		{
			"base fee change denominator is 0 ",
			NewParams(true, 0, 3, math.LegacyNewDec(2000000000), int64(544435345345435345), math.LegacyNewDecWithPrec(20, 4), DefaultMinGasMultiplier, DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			true,
		},
		{
			"invalid: elasticity multiplier is zero",
			NewParams(true, 7, 0, math.LegacyNewDec(2000000000), int64(100), DefaultMinGasPrice, DefaultMinGasMultiplier, DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			true,
		},
		{
			"invalid: enable height negative",
			NewParams(true, 7, 3, math.LegacyNewDec(2000000000), int64(-10), DefaultMinGasPrice, DefaultMinGasMultiplier, DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			true,
		},
		{
			"invalid: base fee negative",
			NewParams(true, 7, 3, math.LegacyNewDec(-2000000000), int64(100), DefaultMinGasPrice, DefaultMinGasMultiplier, DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			true,
		},
		{
			"invalid: min gas price negative",
			NewParams(true, 7, 3, math.LegacyNewDec(2000000000), int64(544435345345435345), math.LegacyNewDecFromInt(math.NewInt(-1)), DefaultMinGasMultiplier, DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			true,
		},
		{
			"valid: min gas multiplier zero",
			NewParams(true, 7, 3, math.LegacyNewDec(2000000000), int64(544435345345435345), DefaultMinGasPrice, math.LegacyZeroDec(), DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			false,
		},
		{
			"invalid: min gas multiplier is negative",
			NewParams(true, 7, 3, math.LegacyNewDec(2000000000), int64(544435345345435345), DefaultMinGasPrice, math.LegacyNewDecWithPrec(-5, 1), DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			true,
		},
		{
			"invalid: min gas multiplier bigger than 1",
			NewParams(true, 7, 3, math.LegacyNewDec(2000000000), int64(544435345345435345), math.LegacyNewDecWithPrec(20, 4), math.LegacyNewDec(2), DefaultMinGasPriceRate, DefaultMinGasPriceRate),
			true,
		},
		{
			"invalid: min gas price rate negative",
			NewParams(true, 7, 3, math.LegacyNewDec(2000000000), int64(544435345345435345), DefaultMinGasPrice, DefaultMinGasMultiplier, math.LegacyNewDecFromInt(math.NewInt(-1)), DefaultMinGasPriceRate),
			true,
		},
	}

	for _, tc := range testCases {
		err := tc.params.Validate()

		if tc.expError {
			suite.Require().Error(err, tc.name)
		} else {
			suite.Require().NoError(err, tc.name)
		}
	}
}

func (suite *ParamsTestSuite) TestParamsValidatePriv() {
	suite.Require().Error(validateMinGasPrice(math.LegacyDec{}))
	suite.Require().Error(validateMinGasMultiplier(math.LegacyNewDec(-5)))
	suite.Require().Error(validateMinGasMultiplier(math.LegacyDec{}))
	suite.Require().Error(validateGasPriceAdjustmentFactor(math.LegacyDec{}))
	suite.Require().Error(validateGasPriceAdjustmentFactor(math.LegacyNewDec(-5)))
}

func (suite *ParamsTestSuite) TestParamsValidateMinGasPrice() {
	testCases := []struct {
		name     string
		value    math.LegacyDec
		expError bool
	}{
		{"default", DefaultParams().MinGasPrice, false},
		{"valid", math.LegacyNewDecFromInt(math.NewInt(1)), false},
		{"invalid - is negative", math.LegacyNewDecFromInt(math.NewInt(-1)), true},
	}

	for _, tc := range testCases {
		err := validateMinGasPrice(tc.value)

		if tc.expError {
			suite.Require().Error(err, tc.name)
		} else {
			suite.Require().NoError(err, tc.name)
		}
	}
}

func (suite *ParamsTestSuite) TestParamsValidateMinGasPriceRate() {
	testCases := []struct {
		name     string
		value    math.LegacyDec
		expError bool
	}{
		{"default", DefaultParams().GasPriceAdjustmentFactor, false},
		{"valid", math.LegacyNewDecFromInt(math.NewInt(1)), false},
		{"valid zero", math.LegacyZeroDec(), false},
		{"invalid - is negative", math.LegacyNewDecFromInt(math.NewInt(-1)), true},
	}

	for _, tc := range testCases {
		err := validateGasPriceAdjustmentFactor(tc.value)

		if tc.expError {
			suite.Require().Error(err, tc.name)
		} else {
			suite.Require().NoError(err, tc.name)
		}
	}
}
