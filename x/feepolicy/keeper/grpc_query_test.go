package keeper_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

func TestQueryModerator(t *testing.T) {
	var (
		nw  *network.UnitTestNetwork
		ctx sdk.Context
	)

	testCases := []struct {
		name    string
		expPass bool
	}{
		{
			"pass",
			true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// reset network and context
			nw = network.NewUnitTestNetwork()
			ctx = nw.GetContext()
			qc := nw.GetFeePolicyClient()

			moderator, err := nw.App.FeePolicyKeeper.GetModeratorAddress(ctx)
			require.NoError(t, err)
			exp := &types.QueryModeratorAddressResponse{ModeratorAddress: moderator.Address}

			res, err := qc.ModeratorAddress(ctx.Context(), &types.QueryModeratorAddressRequest{})
			if tc.expPass {
				require.Equal(t, exp, res, tc.name)
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestQueryDiscounts(t *testing.T) {
	var (
		nw  *network.UnitTestNetwork
		ctx sdk.Context
	)

	testCases := []struct {
		name    string
		expPass bool
	}{
		{
			"pass",
			true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// reset network and context
			nw = network.NewUnitTestNetwork()
			ctx = nw.GetContext()
			qc := nw.GetFeePolicyClient()

			discounts, _, err := nw.App.FeePolicyKeeper.GetPaginatedDiscounts(ctx, &query.PageRequest{
				Limit:      math.MaxUint64,
				CountTotal: true,
			})
			if len(discounts) == 0 {
				discounts = []types.AccountDiscount(nil)
			}
			require.NoError(t, err)

			exp := &types.QueryDiscountsResponse{
				Discounts:  discounts,
				Pagination: &query.PageResponse{},
			}

			res, err := qc.Discounts(ctx.Context(), &types.QueryDiscountsRequest{})
			if tc.expPass {
				require.Equal(t, exp, res, tc.name)
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestQueryDiscount(t *testing.T) {
	var (
		nw  *network.UnitTestNetwork
		ctx sdk.Context
	)

	testCases := []struct {
		name    string
		setup   func(ctx sdk.Context, nw *network.UnitTestNetwork) (string, *types.AccountDiscount)
		expPass bool
	}{
		{
			name: "pass - not found returns empty response",
			setup: func(ctx sdk.Context, nw *network.UnitTestNetwork) (string, *types.AccountDiscount) {
				return "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3", nil
			},
			expPass: true,
		},
		{
			name: "pass - found returns discount",
			setup: func(ctx sdk.Context, nw *network.UnitTestNetwork) (string, *types.AccountDiscount) {
				accDiscount := &types.AccountDiscount{
					Address: "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3",
					Modules: []types.ModuleDiscount{
						{
							Module: "bank",
							Discounts: []types.Discount{
								{
									DiscountType: "percent",
									MsgType:      "/cosmos.bank.v1beta1.MsgSend",
									Amount:       sdkmath.LegacyNewDec(100),
								},
							},
						},
					},
				}
				nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, *accDiscount)
				return accDiscount.Address, accDiscount
			},
			expPass: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// reset network and context
			nw = network.NewUnitTestNetwork()
			ctx = nw.GetContext()
			qc := nw.GetFeePolicyClient()

			addr, expDiscount := tc.setup(ctx, nw)

			res, err := qc.Discount(ctx.Context(), &types.QueryDiscountRequest{Address: addr})
			require.NoError(t, err)

			if expDiscount == nil {
				require.Equal(t, &types.QueryDiscountResponse{}, res)
				return
			}

			require.Equal(t, &types.QueryDiscountResponse{Discount: *expDiscount}, res)
		})
	}
}
