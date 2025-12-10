package types

import (
	"testing"

	"cosmossdk.io/math"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/suite"
)

type MsgsTestSuite struct {
	suite.Suite
}

func TestMsgsTestSuite(t *testing.T) {
	suite.Run(t, new(MsgsTestSuite))
}

func (suite *MsgsTestSuite) TestMsgChangeModerator() {
	testCases := []struct {
		name    string
		msg     *MsgChangeModerator
		expPass bool
	}{
		{
			"fail - invalid moderator address",
			&MsgChangeModerator{
				ModeratorAddress:    "invalid",
				NewModeratorAddress: "invalid",
			},
			false,
		},
		{
			"fail - invalid new moderator",
			&MsgChangeModerator{
				ModeratorAddress:    authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				NewModeratorAddress: "invalid",
			},
			false,
		},
		{
			"pass - valid msg",
			&MsgChangeModerator{
				ModeratorAddress:    authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				NewModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgRegisterDiscount() {
	testCases := []struct {
		name    string
		msg     *MsgRegisterDiscounts
		expPass bool
	}{
		{
			"fail - invalid moderator address",
			&MsgRegisterDiscounts{
				ModeratorAddress: "invalid",
				Discounts:        []AccountDiscount{},
			},
			false,
		},
		{
			"fail - invalid discount",
			&MsgRegisterDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Discounts:        []AccountDiscount{},
			},
			false,
		},
		{
			"fail - invalid discount type",
			&MsgRegisterDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Discounts: []AccountDiscount{
					{
						Address: "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
						Modules: []ModuleDiscount{
							{
								Module: "bank",
								Discounts: []Discount{
									{
										DiscountType: "invalid",
										MsgType:      "/cosmos.bank.v1beta1.MsgSend",
										Amount:       math.LegacyNewDec(100),
									},
								},
							},
						},
					},
				},
			},
			false,
		},
		{
			"fail - invalid address",
			&MsgRegisterDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Discounts: []AccountDiscount{
					{
						Address: "invalid",
						Modules: []ModuleDiscount{
							{
								Module: "bank",
								Discounts: []Discount{
									{
										DiscountType: "invalid",
										MsgType:      "/cosmos.bank.v1beta1.MsgSend",
										Amount:       math.LegacyNewDec(100),
									},
								},
							},
						},
					},
				},
			},
			false,
		},
		{
			"pass - valid msg",
			&MsgRegisterDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Discounts: []AccountDiscount{
					{
						Address: "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
						Modules: []ModuleDiscount{
							{
								Module: "bank",
								Discounts: []Discount{
									{
										DiscountType: "percent",
										MsgType:      "/cosmos.bank.v1beta1.MsgSend",
										Amount:       math.LegacyNewDec(100),
									},
								},
							},
						},
					},
				},
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgRemoveDiscounts() {
	testCases := []struct {
		name    string
		msg     *MsgRemoveDiscounts
		expPass bool
	}{
		{
			"fail - invalid moderator address",
			&MsgRemoveDiscounts{
				ModeratorAddress: "invalid",
				Address:          "invalid",
				Module:           "bank",
				MsgType:          "/cosmos.bank.v1beta1.MsgSend",
			},
			false,
		},
		{
			"pass - valid msg",
			&MsgRemoveDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Address:          "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
				Module:           "bank",
				MsgType:          "/cosmos.bank.v1beta1.MsgSend",
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}
