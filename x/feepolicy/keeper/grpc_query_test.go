package keeper_test

import (
	"testing"

	"math"

	"github.com/stretchr/testify/require"

	"github.com/GPTx-global/guru-v2/v2/testutil/integration/os/network"
	"github.com/GPTx-global/guru-v2/v2/x/feepolicy/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
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
