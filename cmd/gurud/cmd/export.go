package cmd

import (
	"bytes"
	"encoding/json"
	"os"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/spf13/cobra"
)

func patchExportCommand(rootCmd *cobra.Command) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "export" {
			if err := cmd.Flags().MarkHidden(sdkserver.FlagForZeroHeight); err != nil {
				panic(err)
			}
			if err := cmd.Flags().MarkHidden(sdkserver.FlagJailAllowedAddrs); err != nil {
				panic(err)
			}
			patchExportRunE(cmd)
			return
		}
	}
	panic("export command not found")
}

func patchExportRunE(cmd *cobra.Command) {
	runE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		outputDocument, _ := cmd.Flags().GetString(flags.FlagOutputDocument)
		if outputDocument != "" {
			if err := runE(cmd, args); err != nil {
				return err
			}
			return patchExportedGenesisFile(outputDocument)
		}

		out := cmd.OutOrStdout()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		defer cmd.SetOut(out)
		if err := runE(cmd, args); err != nil {
			return err
		}
		patched, err := patchExportedGenesisBytes(buf.Bytes())
		if err != nil {
			return err
		}
		_, err = out.Write(patched)
		return err
	}
}

func patchExportedGenesisFile(path string) error {
	bz, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	patched, err := patchExportedGenesisBytes(bz)
	if err != nil {
		return err
	}
	return os.WriteFile(path, patched, 0o644)
}

func patchExportedGenesisBytes(bz []byte) ([]byte, error) {
	var genesis genutiltypes.AppGenesis
	if err := json.Unmarshal(bz, &genesis); err != nil {
		return nil, err
	}
	ensureExportedGenesisVoteExtensions(&genesis)
	return json.Marshal(&genesis)
}

func ensureExportedGenesisVoteExtensions(genesis *genutiltypes.AppGenesis) {
	if genesis.Consensus == nil {
		genesis.Consensus = &genutiltypes.ConsensusGenesis{}
	}
	if genesis.Consensus.Params == nil {
		genesis.Consensus.Params = cmttypes.DefaultConsensusParams()
	}
	minEnableHeight := genesis.InitialHeight
	if minEnableHeight < 1 {
		minEnableHeight = 1
	}
	if genesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight == 0 ||
		genesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight < minEnableHeight {
		genesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight = minEnableHeight
	}
}
