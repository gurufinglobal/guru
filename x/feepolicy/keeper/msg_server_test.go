package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
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

func TestRemoveDiscounts(t *testing.T) {
	var (
		nw  *network.UnitTestNetwork
		ctx sdk.Context
	)

	const (
		addr1 = "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3"
		addr2 = "guru1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqg3d5d2"
	)

	validModerator := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	baseDiscount := func() types.AccountDiscount {
		return types.AccountDiscount{
			Address: addr1,
			Modules: []types.ModuleDiscount{
				{
					Module: "bank",
					Discounts: []types.Discount{
						{
							DiscountType: "percent",
							MsgType:      "/cosmos.bank.v1beta1.MsgSend",
							Amount:       math.LegacyNewDec(100),
						},
						{
							DiscountType: "fixed",
							MsgType:      "/cosmos.bank.v1beta1.MsgMultiSend",
							Amount:       math.LegacyNewDec(1),
						},
					},
				},
				{
					Module: "staking",
					Discounts: []types.Discount{
						{
							DiscountType: "percent",
							MsgType:      "/cosmos.staking.v1beta1.MsgDelegate",
							Amount:       math.LegacyNewDec(5),
						},
					},
				},
			},
		}
	}

	testCases := []struct {
		name      string
		setup     func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork)
		request   *types.MsgRemoveDiscounts
		assert    func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork)
		expectErr bool
	}{
		{
			name: "fail - wrong moderator",
			setup: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, baseDiscount())
			},
			request: &types.MsgRemoveDiscounts{
				ModeratorAddress: addr2,
				Address:          addr1,
				Module:           "",
				MsgType:          "",
			},
			assert: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				_, ok := nw.App.FeePolicyKeeper.GetAccountDiscounts(ctx, addr1)
				require.True(t, ok)
			},
			expectErr: true,
		},
		{
			name: "pass - remove all discounts for account",
			setup: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, baseDiscount())
			},
			request: &types.MsgRemoveDiscounts{
				ModeratorAddress: validModerator,
				Address:          addr1,
				Module:           "",
				MsgType:          "",
			},
			assert: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				_, ok := nw.App.FeePolicyKeeper.GetAccountDiscounts(ctx, addr1)
				require.False(t, ok)
			},
			expectErr: false,
		},
		{
			name: "pass - remove all discounts for module",
			setup: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, baseDiscount())
			},
			request: &types.MsgRemoveDiscounts{
				ModeratorAddress: validModerator,
				Address:          addr1,
				Module:           "bank",
				MsgType:          "",
			},
			assert: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				_, ok := nw.App.FeePolicyKeeper.GetModuleDiscounts(ctx, addr1, "bank")
				require.False(t, ok)

				_, ok = nw.App.FeePolicyKeeper.GetModuleDiscounts(ctx, addr1, "staking")
				require.True(t, ok)
			},
			expectErr: false,
		},
		{
			name: "pass - remove specific msg type discount",
			setup: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, baseDiscount())
			},
			request: &types.MsgRemoveDiscounts{
				ModeratorAddress: validModerator,
				Address:          addr1,
				Module:           "bank",
				MsgType:          "/cosmos.bank.v1beta1.MsgSend",
			},
			assert: func(t *testing.T, ctx sdk.Context, nw *network.UnitTestNetwork) {
				t.Helper()
				discounts, ok := nw.App.FeePolicyKeeper.GetModuleDiscounts(ctx, addr1, "bank")
				require.True(t, ok)
				require.Len(t, discounts, 1)
				require.NotEqual(t, "/cosmos.bank.v1beta1.MsgSend", discounts[0].MsgType)
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// reset network and context
			nw = network.NewUnitTestNetwork()
			ctx = nw.GetContext()

			if tc.setup != nil {
				tc.setup(t, ctx, nw)
			}

			_, err := nw.App.FeePolicyKeeper.RemoveDiscounts(ctx, tc.request)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tc.assert != nil {
				tc.assert(t, ctx, nw)
			}
		})
	}
}
