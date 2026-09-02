package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"

	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

func TestInitCommandWritesDisabledVoteExtensionsWithoutAnOverride(t *testing.T) {
	require.NoError(t, chainconfig.SetupSDKConfig())

	homePath := t.TempDir()
	cmtcfg.EnsureRoot(homePath)
	serverContext := sdkserver.NewDefaultContext()
	serverContext.Config.SetRoot(homePath)
	commandContext := context.WithValue(
		context.Background(),
		sdkserver.ServerContextKey,
		serverContext,
	)

	command := newInitCommand(homePath)
	command.SetContext(commandContext)
	require.NoError(t, client.SetCmdClientContext(
		command,
		client.Context{}.WithHomeDir(homePath),
	))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{
		"validator",
		"--chain-id", "guru-init-test-1",
		"--constitution-base-address", sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		"--constitution-moderator-address", sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
	})
	require.NoError(t, command.Execute())

	genesis, err := genutiltypes.AppGenesisFromFile(
		filepath.Join(homePath, "config", "genesis.json"),
	)
	require.NoError(t, err)
	require.NotNil(t, genesis.Consensus)
	require.NotNil(t, genesis.Consensus.Params)
	require.NotNil(t, genesis.Consensus.Params.ABCI)
	require.Zero(t, genesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight)

	var initResult map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(output.Bytes(), &initResult))
	require.NotContains(t, initResult, "vote_extensions_enable_height")
}
