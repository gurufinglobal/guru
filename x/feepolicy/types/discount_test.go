package types

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"cosmossdk.io/math"
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
		{
			"invalid: percent discount is greater than 100",
			Discount{
				DiscountType: "percent",
				MsgType:      "/cosmos.bank.v1beta1.MsgSend",
				Amount:       math.LegacyNewDec(101),
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

func (suite *DiscountTestSuite) TestAccountDiscountValidate() {
	testCases := []struct {
		name     string
		discount AccountDiscount
		expError bool
	}{
		{
			name: "valid global discount with empty address",
			discount: AccountDiscount{
				Address: "",
				Modules: []ModuleDiscount{
					{
						Module: "bank",
						Discounts: []Discount{
							{
								DiscountType: "percent",
								MsgType:      "/cosmos.bank.v1beta1.MsgSend",
								Amount:       math.LegacyNewDec(10),
							},
						},
					},
				},
			},
			expError: false,
		},
		{
			name: "invalid account address",
			discount: AccountDiscount{
				Address: "invalid",
				Modules: []ModuleDiscount{
					{
						Module: "bank",
						Discounts: []Discount{
							{
								DiscountType: "percent",
								MsgType:      "/cosmos.bank.v1beta1.MsgSend",
								Amount:       math.LegacyNewDec(10),
							},
						},
					},
				},
			},
			expError: true,
		},
	}

	for _, tc := range testCases {
		err := ValidateAccountDiscount(tc.discount)
		if tc.expError {
			suite.Require().Error(err, tc.name)
		} else {
			suite.Require().NoError(err, tc.name)
		}
	}
}
