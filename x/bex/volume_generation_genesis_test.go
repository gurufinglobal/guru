package bex

import (
	"testing"

	bexv1 "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestVolumeGenerationGenesisValidationAndRoundTrip(t *testing.T) {
	am, ctx := setupAppModule(t)
	valid := validGenesisState(t, am, ctx)

	t.Run("same epoch identity with distinct generations round trips", func(t *testing.T) {
		genesis := mutateGenesis(valid, func(g *bexv1.GenesisState) {
			g.Exchanges[0].VolumeWindowGeneration = 2
			secondGeneration := bexv1.CloneMessage(g.VolumeWindows[0])
			secondGeneration.VolumeWindowGeneration = 2
			secondGeneration.Amount = "7"
			g.VolumeWindows = append(g.VolumeWindows, secondGeneration)
		})
		require.NoError(t, am.validateGenesisState(ctx, genesis))

		target := newMemoryGenesisTarget()
		require.NoError(t, writeGenesisState(target.target, genesis))
		roundTrip, err := readGenesisState(target.source, am.defaultGenesisState())
		require.NoError(t, err)
		require.NoError(t, am.validateGenesisState(ctx, roundTrip))
		require.Len(t, roundTrip.GetVolumeWindows(), 2)

		amountByGeneration := make(map[uint64]string, len(roundTrip.GetVolumeWindows()))
		for _, window := range roundTrip.GetVolumeWindows() {
			require.Equal(t, genesis.GetVolumeWindows()[0].GetExchangeId(), window.GetExchangeId())
			require.Equal(t, genesis.GetVolumeWindows()[0].GetDirection(), window.GetDirection())
			require.Equal(t, genesis.GetVolumeWindows()[0].GetEpochStartUnix(), window.GetEpochStartUnix())
			require.Equal(t, genesis.GetVolumeWindows()[0].GetEpochSeconds(), window.GetEpochSeconds())
			amountByGeneration[window.GetVolumeWindowGeneration()] = window.GetAmount()
		}
		require.Equal(t, map[uint64]string{1: "5", 2: "7"}, amountByGeneration)
	})

	t.Run("same generation duplicate is rejected", func(t *testing.T) {
		duplicate := mutateGenesis(valid, func(g *bexv1.GenesisState) {
			g.VolumeWindows = append(
				g.VolumeWindows,
				bexv1.CloneMessage(g.VolumeWindows[0]),
			)
		})
		requireGenesisInvalid(t, am, ctx, duplicate)
	})

	t.Run("zero generation is rejected", func(t *testing.T) {
		zeroGeneration := mutateGenesis(valid, func(g *bexv1.GenesisState) {
			g.VolumeWindows[0].VolumeWindowGeneration = 0
		})
		requireGenesisInvalid(t, am, ctx, zeroGeneration)
	})

	t.Run("generation greater than exchange is rejected", func(t *testing.T) {
		futureGeneration := mutateGenesis(valid, func(g *bexv1.GenesisState) {
			g.VolumeWindows[0].VolumeWindowGeneration = g.Exchanges[0].GetVolumeWindowGeneration() + 1
		})
		requireGenesisInvalid(t, am, ctx, futureGeneration)
	})
}
