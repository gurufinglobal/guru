package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
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

func TestDefaultAppTomlEnablesBoundedCosmosMempool(t *testing.T) {
	_, rawConfig := defaultAppToml()
	config, ok := rawConfig.(guruConfig)
	require.True(t, ok)
	require.Equal(t, defaultCosmosMempoolMaxTxs, config.Mempool.MaxTxs)
}

func TestDefaultAppTomlDisablesInsecureJSONRPCUnlock(t *testing.T) {
	_, rawConfig := defaultAppToml()
	config, ok := rawConfig.(guruConfig)
	require.True(t, ok)
	require.False(t, config.JSONRPC.AllowInsecureUnlock)
}

func TestDefaultCometConfigUsesSupportedGoLevelDB(t *testing.T) {
	require.Equal(t, "goleveldb", defaultConfigToml().DBBackend)
}

func TestDefaultCometConfigUsesFloodMempool(t *testing.T) {
	require.Equal(t, cmtcfg.MempoolTypeFlood, defaultConfigToml().Mempool.Type)
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
