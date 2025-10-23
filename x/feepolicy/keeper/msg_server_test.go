package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func TestChangeModerator(t *testing.T) {
	var (
		nw  *network.UnitTestNetwork
		ctx sdk.Context
	)

	testCases := []struct {
		name      string
		request   *types.MsgChangeModerator
		expectErr bool
	}{
		{
			name:      "fail - invalid authority",
			request:   &types.MsgChangeModerator{ModeratorAddress: "invalid"},
			expectErr: true,
		},
		{
			name: "pass - valid msg",
			request: &types.MsgChangeModerator{
				ModeratorAddress:    authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				NewModeratorAddress: "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// reset network and context
			nw = network.NewUnitTestNetwork()
			ctx = nw.GetContext()

			_, err := nw.App.FeePolicyKeeper.ChangeModerator(ctx, tc.request)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRegisterDiscounts(t *testing.T) {
	var (
		nw  *network.UnitTestNetwork
		ctx sdk.Context
	)

	testCases := []struct {
		name      string
		request   *types.MsgRegisterDiscounts
		expectErr bool
	}{
		{
			name:      "fail - invalid authority",
			request:   &types.MsgRegisterDiscounts{ModeratorAddress: "invalid"},
			expectErr: true,
		},
		{
			name: "pass - valid msg",
			request: &types.MsgRegisterDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Discounts: []types.AccountDiscount{
					{
						Address: "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
						Modules: []types.ModuleDiscount{
							{
								Module: "bank",
								Discounts: []types.Discount{
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

			expectErr: false,
		},
		{
			name: "fail - invalid msg - negative amount",
			request: &types.MsgRegisterDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Discounts: []types.AccountDiscount{
					{
						Address: "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
						Modules: []types.ModuleDiscount{
							{
								Module: "bank",
								Discounts: []types.Discount{
									{
										DiscountType: "percent",
										MsgType:      "/cosmos.bank.v1beta1.MsgSend",
										Amount:       math.LegacyNewDec(-100),
									},
								},
							},
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "fail - invalid msg - 0 amount",
			request: &types.MsgRegisterDiscounts{
				ModeratorAddress: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Discounts: []types.AccountDiscount{
					{
						Address: "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
						Modules: []types.ModuleDiscount{
							{
								Module: "bank",
								Discounts: []types.Discount{
									{
										DiscountType: "percent",
										MsgType:      "/cosmos.bank.v1beta1.MsgSend",
										Amount:       math.LegacyNewDec(0),
									},
								},
							},
						},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// reset network and context
			nw = network.NewUnitTestNetwork()
			ctx = nw.GetContext()

			_, err := nw.App.FeePolicyKeeper.RegisterDiscounts(ctx, tc.request)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
