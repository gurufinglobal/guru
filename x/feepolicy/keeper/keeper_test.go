package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

func TestKeeperStoreOperations(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	const addr = "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3"

	accDiscount := types.AccountDiscount{
		Address: addr,
		Modules: []types.ModuleDiscount{
			{
				Module: "m1",
				Discounts: []types.Discount{
					{DiscountType: "percent", MsgType: "msg1", Amount: sdkmath.LegacyNewDec(100)},
				},
			},
			{
				Module: "m2",
				Discounts: []types.Discount{
					{DiscountType: "fixed", MsgType: "msg2", Amount: sdkmath.LegacyNewDec(1)},
				},
			},
		},
	}

	nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, accDiscount)

	got, ok := nw.App.FeePolicyKeeper.GetAccountDiscounts(ctx, addr)
	require.True(t, ok)
	require.Equal(t, accDiscount, got)

	m1Discounts, ok := nw.App.FeePolicyKeeper.GetModuleDiscounts(ctx, addr, "m1")
	require.True(t, ok)
	require.Len(t, m1Discounts, 1)

	nw.App.FeePolicyKeeper.DeleteModuleDiscounts(ctx, addr, "m1")

	_, ok = nw.App.FeePolicyKeeper.GetModuleDiscounts(ctx, addr, "m1")
	require.False(t, ok)

	_, ok = nw.App.FeePolicyKeeper.GetModuleDiscounts(ctx, addr, "m2")
	require.True(t, ok)

	nw.App.FeePolicyKeeper.DeleteAccountDiscounts(ctx, addr)
	_, ok = nw.App.FeePolicyKeeper.GetAccountDiscounts(ctx, addr)
	require.False(t, ok)
}

func TestKeeperDeleteMsgTypeDiscounts(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	const addr = "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3"

	accDiscount := types.AccountDiscount{
		Address: addr,
		Modules: []types.ModuleDiscount{
			{
				Module: "bank",
				Discounts: []types.Discount{
					{DiscountType: "percent", MsgType: "/cosmos.bank.v1beta1.MsgSend", Amount: sdkmath.LegacyNewDec(100)},
					{DiscountType: "fixed", MsgType: "/cosmos.bank.v1beta1.MsgMultiSend", Amount: sdkmath.LegacyNewDec(1)},
				},
			},
		},
	}
	nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, accDiscount)

	nw.App.FeePolicyKeeper.DeleteMsgTypeDiscounts(ctx, addr, "/cosmos.bank.v1beta1.MsgSend")

	discounts, ok := nw.App.FeePolicyKeeper.GetModuleDiscounts(ctx, addr, "bank")
	require.True(t, ok)
	require.Len(t, discounts, 1)
	require.Equal(t, "/cosmos.bank.v1beta1.MsgMultiSend", discounts[0].MsgType)
}

func TestKeeperGetDiscountAndLogger(t *testing.T) {
	nw := network.NewUnitTestNetwork()
	ctx := nw.GetContext()

	const (
		addr1 = "guru1gzsvk8rruqn2sx64acfsskrwy8hvrmaf6dvhj3"
		addr2 = "guru1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqg3d5d2"
	)

	// Ensure logger call is covered.
	_ = nw.App.FeePolicyKeeper.Logger(ctx)

	accDiscount := types.AccountDiscount{
		Address: addr1,
		Modules: []types.ModuleDiscount{
			{
				Module: "testmodule",
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
	nw.App.FeePolicyKeeper.SetAccountDiscounts(ctx, accDiscount)

	msg := &banktypes.MsgSend{
		FromAddress: addr1,
		ToAddress:   addr2,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
	}

	discount := nw.App.FeePolicyKeeper.GetDiscount(ctx, addr1, []sdk.Msg{msg})
	require.Equal(t, accDiscount.Modules[0].Discounts[0], discount)

	discount = nw.App.FeePolicyKeeper.GetDiscount(ctx, addr2, []sdk.Msg{msg})
	require.Equal(t, types.Discount{}, discount)
}
