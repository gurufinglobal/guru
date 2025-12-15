package feepolicy_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	feepolicy "github.com/gurufinglobal/guru/v2/x/feepolicy"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

func TestInitGenesisAndExportGenesis(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	authority := nw.App.FeePolicyKeeper.GetAuthority()

	gs := types.GenesisState{
		ModeratorAddress: "",
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
								Amount:       sdkmath.LegacyNewDec(100),
							},
						},
					},
				},
			},
		},
	}

	feepolicy.InitGenesis(ctx, nw.App.FeePolicyKeeper, gs)

	moderator, err := nw.App.FeePolicyKeeper.GetModeratorAddress(ctx)
	require.NoError(t, err)
	require.Equal(t, authority, moderator.Address)

	exported := feepolicy.ExportGenesis(ctx, nw.App.FeePolicyKeeper)
	require.Equal(t, authority, exported.ModeratorAddress)
	require.Len(t, exported.Discounts, 1)
	require.Equal(t, gs.Discounts[0], exported.Discounts[0])
}
