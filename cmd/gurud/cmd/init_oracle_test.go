package cmd

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/evm/crypto/hd"
	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
)

func TestRootCmdUsesEthereumCoinTypeAndEnablesVoteExtensionsAtHeightOne(t *testing.T) {
	home := t.TempDir()
	rootCmd := NewRootCmd()
	require.Equal(t, uint32(sdk.Purpose), sdk.GetConfig().GetPurpose())
	require.Equal(t, hd.Bip44CoinType, sdk.GetConfig().GetCoinType())
	require.Equal(t, "m/44'/60'/0'/0/0", sdk.GetConfig().GetFullBIP44Path())
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{
		"init",
		"validator",
		"--" + flags.FlagHome,
		home,
		"--" + flags.FlagChainID,
		"guru_631",
		"--" + FlagConstitutionBaseAddress,
		initOracleAddress(t, 0x11),
		"--" + FlagConstitutionModeratorAddress,
		initOracleAddress(t, 0x22),
	})

	require.NoError(t, rootCmd.Execute())

	genesis, err := types.AppGenesisFromFile(filepath.Join(home, "config", "genesis.json"))
	require.NoError(t, err)
	require.NotNil(t, genesis.Consensus)
	require.NotNil(t, genesis.Consensus.Params)
	require.NotNil(t, genesis.Consensus.Params.ABCI)
	require.Equal(t, int64(1), genesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}

func initOracleAddress(t *testing.T, b byte) string {
	t.Helper()

	address, err := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr).BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}
