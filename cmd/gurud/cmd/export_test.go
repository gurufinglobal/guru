package cmd

import (
	"encoding/json"
	"testing"

	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

func TestPatchExportedGenesisBytesEnablesVoteExtensions(t *testing.T) {
	genesis := genutiltypes.AppGenesis{
		InitialHeight: 12,
		Consensus:     &genutiltypes.ConsensusGenesis{Params: cmttypes.DefaultConsensusParams()},
	}
	bz, err := json.Marshal(&genesis)
	require.NoError(t, err)

	patched, err := patchExportedGenesisBytes(bz)
	require.NoError(t, err)

	var out genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(patched, &out))
	require.NotNil(t, out.Consensus)
	require.NotNil(t, out.Consensus.Params)
	require.Equal(t, int64(12), out.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}

func TestPatchExportedGenesisBytesPreservesLaterVoteExtensionHeight(t *testing.T) {
	params := cmttypes.DefaultConsensusParams()
	params.ABCI.VoteExtensionsEnableHeight = 9
	genesis := genutiltypes.AppGenesis{
		InitialHeight: 5,
		Consensus:     &genutiltypes.ConsensusGenesis{Params: params},
	}
	bz, err := json.Marshal(&genesis)
	require.NoError(t, err)

	patched, err := patchExportedGenesisBytes(bz)
	require.NoError(t, err)

	var out genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(patched, &out))
	require.Equal(t, int64(9), out.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}
