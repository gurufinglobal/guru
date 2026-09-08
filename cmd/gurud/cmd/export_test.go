package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"cosmossdk.io/log"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

func TestAppExportRejectsInvalidRequestsBeforeAppCreation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		zero          bool
		jail, modules []string
		want          string
	}{
		{"jail without zero height", false, []string{"validator"}, nil, "jail allowlist requires zero-height export"},
		{"partial export", true, nil, []string{"bank"}, "zero-height export requires a complete module export"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := appExport(log.NewNopLogger(), nil, nil, -1, tc.zero, tc.jail, nil, tc.modules)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestNormalExportUsesUpstreamGenesisSemantics(t *testing.T) {
	homePath := t.TempDir()
	configPath := filepath.Join(homePath, "config")
	require.NoError(t, os.MkdirAll(configPath, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(homePath, "data"), 0o700))

	sourceGenesis := genutiltypes.AppGenesis{
		ChainID:       "source-chain",
		InitialHeight: 8,
		AppHash:       []byte{0x01, 0x02, 0x03},
		AppState:      json.RawMessage(`{"source":true}`),
		Consensus: &genutiltypes.ConsensusGenesis{
			Params: cmttypes.DefaultConsensusParams(),
		},
	}
	sourceBytes, err := json.Marshal(sourceGenesis)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configPath, "genesis.json"), sourceBytes, 0o600))

	params := cmttypes.DefaultConsensusParams()
	params.Version.App = 17
	params.ABCI.VoteExtensionsEnableHeight = 7
	exporterCalled := false
	exporter := func(
		_ log.Logger,
		database dbm.DB,
		_ io.Writer,
		_ int64,
		forZeroHeight bool,
		jailAllowedAddrs []string,
		_ servertypes.AppOptions,
		modulesToExport []string,
	) (servertypes.ExportedApp, error) {
		exporterCalled = true
		require.False(t, forZeroHeight)
		require.Empty(t, jailAllowedAddrs)
		require.Empty(t, modulesToExport)
		require.NoError(t, database.Close())
		return servertypes.ExportedApp{
			AppState:        json.RawMessage(`{"exported":true}`),
			Height:          42,
			ConsensusParams: params.ToProto(),
		}, nil
	}

	command := server.ExportCmd(exporter, homePath)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetArgs([]string{"--home", homePath})
	serverContext := server.NewDefaultContext()
	serverContext.Viper.Set(flags.FlagHome, homePath)
	serverContext.Viper.Set(flags.FlagChainID, "different-runtime-chain")
	ctx := context.WithValue(context.Background(), server.ServerContextKey, serverContext)
	require.NoError(t, command.ExecuteContext(ctx))
	require.True(t, exporterCalled)

	var exportedGenesis genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &exportedGenesis))
	require.Equal(t, "source-chain", exportedGenesis.ChainID)
	require.True(t, exportedGenesis.GenesisTime.IsZero())
	require.Equal(t, []byte{0x01, 0x02, 0x03}, exportedGenesis.AppHash)
	require.Equal(t, int64(42), exportedGenesis.InitialHeight)
	require.JSONEq(t, `{"exported":true}`, string(exportedGenesis.AppState))
	require.Zero(t, exportedGenesis.Consensus.Params.Version.App)
	require.Zero(t, exportedGenesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}
