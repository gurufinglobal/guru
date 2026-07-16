package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
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

func writeChainIDGenesis(t *testing.T, chainID string) string {
	t.Helper()

	home := t.TempDir()
	configDir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "genesis.json"),
		[]byte(`{"chain_id":"`+chainID+`"}`),
		0o600,
	))
	return home
}

type testAppOptions map[string]any

func (o testAppOptions) Get(key string) any {
	return o[key]
}
