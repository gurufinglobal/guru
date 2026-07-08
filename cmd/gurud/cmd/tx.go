package cmd

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	bexcli "github.com/gurufinglobal/guru/v3/x/bex/client/cli"
	constitutioncli "github.com/gurufinglobal/guru/v3/x/constitution/client/cli"
	oraclecli "github.com/gurufinglobal/guru/v3/x/oracle/client/cli"
	"github.com/spf13/cobra"
)

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		authcmd.GetSimulateCmd(),
		bexcli.GetTxCmd(),
		constitutioncli.GetTxCmd(),
		oraclecli.GetTxCmd(),
	)

	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

	return cmd
}
