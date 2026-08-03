package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	cosmosevmserver "github.com/cosmos/evm/server"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestGetChainIDFromOptsUsesGenesisWhenFlagIsEmpty(t *testing.T) {
	home := writeChainIDGenesis(t, "guru_631")

	chainID, err := getChainIDFromOpts(testAppOptions{
		flags.FlagHome:    home,
		flags.FlagChainID: "",
	})

	require.NoError(t, err)
	require.Equal(t, "guru_631", chainID)
}

func TestGetChainIDFromOptsAcceptsMatchingFlag(t *testing.T) {
	home := writeChainIDGenesis(t, "guru_631")

	chainID, err := getChainIDFromOpts(testAppOptions{
		flags.FlagHome:    home,
		flags.FlagChainID: "guru_631",
	})

	require.NoError(t, err)
	require.Equal(t, "guru_631", chainID)
}

func TestGetChainIDFromOptsRejectsFlagMismatch(t *testing.T) {
	home := writeChainIDGenesis(t, "guru_631")

	_, err := getChainIDFromOpts(testAppOptions{
		flags.FlagHome:    home,
		flags.FlagChainID: "wrong-chain",
	})

	require.ErrorContains(t, err, `configured chain ID "wrong-chain" does not match genesis chain ID "guru_631"`)
}

func TestGetChainIDFromOptsRequiresGenesis(t *testing.T) {
	_, err := getChainIDFromOpts(testAppOptions{
		flags.FlagHome: t.TempDir(),
	})

	require.ErrorContains(t, err, "load chain ID from genesis")
}

func TestDefaultAppTomlEnablesGRPCForOraclePreflight(t *testing.T) {
	_, rawConfig := defaultAppToml()
	config, ok := rawConfig.(guruConfig)
	require.True(t, ok)
	require.True(t, config.GRPC.Enable)
}

func TestDefaultAppTomlDisablesInsecureJSONRPCUnlock(t *testing.T) {
	_, rawConfig := defaultAppToml()
	config, ok := rawConfig.(guruConfig)
	require.True(t, ok)
	require.False(t, config.JSONRPC.AllowInsecureUnlock)
}

func TestDefaultAppTomlUsesBlockSTMDefaults(t *testing.T) {
	_, rawConfig := defaultAppToml()
	config, ok := rawConfig.(guruConfig)
	require.True(t, ok)

	require.Equal(t, serverconfig.BlockExecutorBlockSTM, config.BlockExecutor)
	require.Zero(t, config.BlockSTMWorkers)
	require.True(t, config.BlockSTMPreEstimate)
}

func TestAddModuleInitFlagsUsesGuruBlockSTMDefaults(t *testing.T) {
	cmd := &cobra.Command{}
	addModuleInitFlags(cmd)

	executor, err := cmd.Flags().GetString(sdkserver.FlagBlockExecutor)
	require.NoError(t, err)
	require.Equal(t, serverconfig.BlockExecutorBlockSTM, executor)

	workers, err := cmd.Flags().GetInt(sdkserver.FlagBlockSTMWorkers)
	require.NoError(t, err)
	require.Zero(t, workers)

	preEstimate, err := cmd.Flags().GetBool(sdkserver.FlagBlockSTMPreEstimate)
	require.NoError(t, err)
	require.True(t, preEstimate)
}

func TestAddModuleInitFlagsRejectsInvalidExecutorBeforeRun(t *testing.T) {
	cmd := &cobra.Command{}
	addModuleInitFlags(cmd)

	serverCtx := sdkserver.NewDefaultContext()
	serverCtx.Viper.Set(sdkserver.FlagMinGasPrices, "0uguru")
	serverCtx.Viper.Set(sdkserver.FlagBlockExecutor, "invalid")
	cmd.SetContext(context.WithValue(context.Background(), sdkserver.ServerContextKey, serverCtx))

	err := cmd.PreRunE(cmd, nil)
	require.ErrorContains(t, err, `invalid block executor "invalid"`)
}

func TestAddModuleInitFlagsPreservesExistingPreRunE(t *testing.T) {
	expectedErr := errors.New("existing pre-run failed")
	called := false
	cmd := &cobra.Command{
		PreRunE: func(_ *cobra.Command, _ []string) error {
			called = true
			return expectedErr
		},
	}
	addModuleInitFlags(cmd)

	err := cmd.PreRunE(cmd, nil)
	require.ErrorIs(t, err, expectedErr)
	require.True(t, called)
}

func TestAddModuleInitFlagsRejectsNegativeWorkersBeforeRun(t *testing.T) {
	cmd := &cobra.Command{}
	addModuleInitFlags(cmd)

	serverCtx := sdkserver.NewDefaultContext()
	serverCtx.Viper.Set(sdkserver.FlagMinGasPrices, "0uguru")
	serverCtx.Viper.Set(sdkserver.FlagBlockExecutor, serverconfig.BlockExecutorBlockSTM)
	serverCtx.Viper.Set(sdkserver.FlagBlockSTMWorkers, -1)
	cmd.SetContext(context.WithValue(context.Background(), sdkserver.ServerContextKey, serverCtx))

	err := cmd.PreRunE(cmd, nil)
	require.ErrorContains(t, err, "invalid block-stm-workers -1")
}

func TestStartRejectsInvalidBlockExecutorWithoutPanic(t *testing.T) {
	cmd := cosmosevmserver.StartCmd(cosmosevmserver.StartOptions{})
	addModuleInitFlags(cmd)

	runCalled := false
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		runCalled = true
		return nil
	}

	serverCtx := sdkserver.NewDefaultContext()
	serverCtx.Viper.Set(sdkserver.FlagMinGasPrices, "0uguru")
	ctx := context.WithValue(context.Background(), sdkserver.ServerContextKey, serverCtx)
	cmd.SetArgs([]string{"--" + sdkserver.FlagBlockExecutor, "invalid"})

	err := cmd.ExecuteContext(ctx)
	require.ErrorContains(t, err, "invalid block executor")
	require.False(t, runCalled)
}

func writeChainIDGenesis(t *testing.T, chainID string) string {
	t.Helper()

	home := t.TempDir()
	configDir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	genesis, err := json.Marshal(genutiltypes.AppGenesis{
		ChainID:   chainID,
		Consensus: &genutiltypes.ConsensusGenesis{Params: cmttypes.DefaultConsensusParams()},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "genesis.json"),
		genesis,
		0o600,
	))
	return home
}

type testAppOptions map[string]any

func (o testAppOptions) Get(key string) any {
	return o[key]
}
