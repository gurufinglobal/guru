package types

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/suite"
)

type DiscountTestSuite struct {
	suite.Suite
}

func TestDiscountTestSuite(t *testing.T) {
	suite.Run(t, new(DiscountTestSuite))
}

func (suite *DiscountTestSuite) TestDiscountValidate() {
	testCases := []struct {
		name     string
		discount Discount
		expError bool
	}{
		{"empty", Discount{}, true},
		{
			"valid",
			Discount{
				DiscountType: "percent",
				MsgType:      "/cosmos.bank.v1beta1.MsgSend",
				Amount:       math.LegacyNewDec(100),
			},
			false,
		},
		{
			"invalid: discount type is invalid",
			Discount{
				DiscountType: "invalid",
				MsgType:      "/cosmos.bank.v1beta1.MsgSend",
				Amount:       math.LegacyNewDec(100),
			},
			true,
		},
		{
			"invalid: msg type is empty",
			Discount{
				DiscountType: "percent",
				MsgType:      "",
				Amount:       math.LegacyNewDec(100),
			},
			true,
		},
		{
			"invalid: amount is negative",
			Discount{
				DiscountType: "percent",
				MsgType:      "/cosmos.bank.v1beta1.MsgSend",
				Amount:       math.LegacyNewDec(-100),
			},
			true,
		},
		{
			"invalid: amount is zero",
			Discount{
				DiscountType: "percent",
				MsgType:      "/cosmos.bank.v1beta1.MsgSend",
				Amount:       math.LegacyNewDec(0),
			},
			true,
		},
	}

	for _, tc := range testCases {
		err := ValidateFeeDiscount(tc.discount)

		if tc.expError {
			suite.Require().Error(err, tc.name)
		} else {
			suite.Require().NoError(err, tc.name)
		}
	}
}
