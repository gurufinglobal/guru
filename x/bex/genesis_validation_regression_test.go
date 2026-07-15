package bex

import (
	"math"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestGenesisRejectsCrossRouteDenomCollisions(t *testing.T) {
	am, ctx := setupAppModule(t)
	tests := []struct {
		name   string
		mutate func(t *testing.T, exchange *bexv1.Exchange)
	}{
		{
			name: "denom_a_matches_derived_ibc_denom_b",
			mutate: func(t *testing.T, exchange *bexv1.Exchange) {
				t.Helper()
				exchange.DenomA = exchange.GetIbcDenomB()
				ibcDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis(
					exchange.GetDenomA(),
					exchange.GetPortA(),
					exchange.GetChannelA(),
				)
				require.NoError(t, err)
				exchange.IbcDenomA = ibcDenomA
			},
		},
		{
			name: "denom_b_matches_derived_ibc_denom_a",
			mutate: func(t *testing.T, exchange *bexv1.Exchange) {
				t.Helper()
				exchange.DenomB = exchange.GetIbcDenomA()
				ibcDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis(
					exchange.GetDenomB(),
					exchange.GetPortB(),
					exchange.GetChannelB(),
				)
				require.NoError(t, err)
				exchange.IbcDenomB = ibcDenomB
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := validGenesisState(t, am, ctx)
			tc.mutate(t, genesis.Exchanges[0])
			require.ErrorIs(t, am.validateGenesisState(ctx, genesis), types.ErrInvalidGenesis)
		})
	}
}

func TestGenesisRejectsPendingEffectiveAtBeyondInt64(t *testing.T) {
	am, ctx := setupAppModule(t)
	genesis := validGenesisState(t, am, ctx)
	genesis.Exchanges[0].PendingVolumeEpochSeconds = 2 * 86400
	genesis.Exchanges[0].PendingVolumeEpochEffectiveAtUnix = uint64(math.MaxInt64) + 1

	require.ErrorIs(t, am.validateGenesisState(ctx, genesis), types.ErrInvalidGenesis)
}

func TestGenesisRejectsNonCanonicalFeeCoinLists(t *testing.T) {
	am, ctx := setupAppModule(t)
	tests := []struct {
		name   string
		coins  []*basev1beta1.Coin
		mutate func(*bexv1.GenesisState, []*basev1beta1.Coin)
	}{
		{
			name: "unsorted_collected",
			coins: []*basev1beta1.Coin{
				{Denom: "gxusd", Amount: "1"},
				{Denom: "agxn", Amount: "10"},
			},
			mutate: func(genesis *bexv1.GenesisState, coins []*basev1beta1.Coin) {
				genesis.CollectedFees[0].Coins = coins
			},
		},
		{
			name: "duplicate_collected",
			coins: []*basev1beta1.Coin{
				{Denom: "agxn", Amount: "5"},
				{Denom: "agxn", Amount: "5"},
			},
			mutate: func(genesis *bexv1.GenesisState, coins []*basev1beta1.Coin) {
				genesis.CollectedFees[0].Coins = coins
			},
		},
		{
			name:  "noncanonical_collected_amount",
			coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "010"}},
			mutate: func(genesis *bexv1.GenesisState, coins []*basev1beta1.Coin) {
				genesis.CollectedFees[0].Coins = coins
			},
		},
		{
			name: "unsorted_locked",
			coins: []*basev1beta1.Coin{
				{Denom: "gxusd", Amount: "1"},
				{Denom: "agxn", Amount: "2"},
			},
			mutate: func(genesis *bexv1.GenesisState, coins []*basev1beta1.Coin) {
				genesis.LockedFees[0].Coins = coins
			},
		},
		{
			name: "duplicate_locked",
			coins: []*basev1beta1.Coin{
				{Denom: "agxn", Amount: "1"},
				{Denom: "agxn", Amount: "1"},
			},
			mutate: func(genesis *bexv1.GenesisState, coins []*basev1beta1.Coin) {
				genesis.LockedFees[0].Coins = coins
			},
		},
		{
			name:  "noncanonical_locked_amount",
			coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "02"}},
			mutate: func(genesis *bexv1.GenesisState, coins []*basev1beta1.Coin) {
				genesis.LockedFees[0].Coins = coins
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := validGenesisState(t, am, ctx)
			tc.mutate(genesis, tc.coins)
			require.ErrorIs(t, am.validateGenesisState(ctx, genesis), types.ErrInvalidGenesis)
		})
	}
}
