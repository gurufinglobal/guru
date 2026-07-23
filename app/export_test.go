package app

import (
	"testing"

	"cosmossdk.io/log/v2"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

func TestExportAppStateAndValidatorsRejectsUnsupportedZeroHeightOptions(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})

	tests := []struct {
		name             string
		forZeroHeight    bool
		jailAllowedAddrs []string
		expectedError    string
	}{
		{
			name:          "zero height",
			forZeroHeight: true,
			expectedError: "zero-height export is not supported",
		},
		{
			name:             "jail allowlist",
			jailAllowedAddrs: []string{"guruvaloper1unsupported"},
			expectedError:    "--jail-allowed-addrs is not supported",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testApp.ExportAppStateAndValidators(tc.forZeroHeight, tc.jailAllowedAddrs, nil)
			require.ErrorContains(t, err, tc.expectedError)
		})
	}
}

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
