package app

import (
	"testing"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestEnsureExportConsensusParamsKeepsVoteExtensionsEnabled(t *testing.T) {
	params := ensureExportConsensusParams(tmproto.ConsensusParams{}, 1)
	require.NotNil(t, params.Abci)
	require.Equal(t, int64(1), params.Abci.VoteExtensionsEnableHeight)
}

func TestEnsureExportConsensusParamsDoesNotEnableBeforeExportInitialHeight(t *testing.T) {
	params := ensureExportConsensusParams(tmproto.ConsensusParams{
		Abci: &tmproto.ABCIParams{VoteExtensionsEnableHeight: 7},
	}, 12)
	require.Equal(t, int64(12), params.Abci.VoteExtensionsEnableHeight)
}

func TestEnsureExportConsensusParamsPreservesLaterVoteExtensionHeight(t *testing.T) {
	params := ensureExportConsensusParams(tmproto.ConsensusParams{
		Abci: &tmproto.ABCIParams{VoteExtensionsEnableHeight: 12},
	}, 7)
	require.Equal(t, int64(12), params.Abci.VoteExtensionsEnableHeight)
}
