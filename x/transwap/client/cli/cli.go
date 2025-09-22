package cli

import (
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the query commands for IBC connections
func GetQueryCmd() *cobra.Command {
	queryCmd := &cobra.Command{
		Use:                        "ibc-transfer",
		Short:                      "IBC fungible token transfer query subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
	}

	queryCmd.AddCommand(
		GetCmdQueryDenom(),
		GetCmdQueryDenoms(),
		GetCmdParams(),
		GetCmdQueryEscrowAddress(),
		GetCmdQueryDenomHash(),
		GetCmdQueryTotalEscrowForDenom(),
	)

	return queryCmd
}
