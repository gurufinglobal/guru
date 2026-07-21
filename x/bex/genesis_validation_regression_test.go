package bex

import (
	"math"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestGenesisRejectsCrossRouteDenomCollisions(t *testing.T) {
	am, ctx := setupAppModule(t)
	tests := []struct {
		name   string
		mutate func(t *testing.T, exchange *types.Exchange)
	}{
		{
			name: "denom_a_matches_derived_ibc_denom_b",
			mutate: func(t *testing.T, exchange *types.Exchange) {
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
			mutate: func(t *testing.T, exchange *types.Exchange) {
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
		coins  sdk.Coins
		mutate func(*types.GenesisState, sdk.Coins)
	}{
		{
			name: "unsorted_collected",
			coins: sdk.Coins{
				sdk.NewInt64Coin("gxusd", 1),
				sdk.NewInt64Coin("agxn", 10),
			},
			mutate: func(genesis *types.GenesisState, coins sdk.Coins) {
				genesis.CollectedFees[0].Coins = coins
			},
		},
		{
			name: "duplicate_collected",
			coins: sdk.Coins{
				sdk.NewInt64Coin("agxn", 5),
				sdk.NewInt64Coin("agxn", 5),
			},
			mutate: func(genesis *types.GenesisState, coins sdk.Coins) {
				genesis.CollectedFees[0].Coins = coins
			},
		},
		{
			name:  "zero_collected_amount",
			coins: sdk.Coins{{Denom: "agxn", Amount: sdkmath.ZeroInt()}},
			mutate: func(genesis *types.GenesisState, coins sdk.Coins) {
				genesis.CollectedFees[0].Coins = coins
			},
		},
		{
			name: "unsorted_locked",
			coins: sdk.Coins{
				sdk.NewInt64Coin("gxusd", 1),
				sdk.NewInt64Coin("agxn", 2),
			},
			mutate: func(genesis *types.GenesisState, coins sdk.Coins) {
				genesis.LockedFees[0].Coins = coins
			},
		},
		{
			name: "duplicate_locked",
			coins: sdk.Coins{
				sdk.NewInt64Coin("agxn", 1),
				sdk.NewInt64Coin("agxn", 1),
			},
			mutate: func(genesis *types.GenesisState, coins sdk.Coins) {
				genesis.LockedFees[0].Coins = coins
			},
		},
		{
			name:  "zero_locked_amount",
			coins: sdk.Coins{{Denom: "agxn", Amount: sdkmath.ZeroInt()}},
			mutate: func(genesis *types.GenesisState, coins sdk.Coins) {
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
