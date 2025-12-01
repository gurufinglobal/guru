package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/gurufinglobal/guru/v2/x/bex/types"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the cli query commands for the xmsquare module.
func GetQueryCmd() *cobra.Command {
	bexQueryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the bex module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	bexQueryCmd.AddCommand(
		GetCmdQueryModeratorAddress(),
		GetCmdQueryExchanges(),
		GetCmdQueryIsAdmin(),
		GetCmdQueryNextExchangeId(),
		GetCmdQueryRatemeter(),
		GetCmdQueryCollectedFees(),
	)

	return bexQueryCmd
}

func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(NewRegisterAdminTxCmd())
	cmd.AddCommand(NewRemoveAdminTxCmd())
	cmd.AddCommand(NewRegisterExchangeTxCmd())
	cmd.AddCommand(NewUpdateExchangeTxCmd())
	cmd.AddCommand(NewUpdateRatemeterTxCmd())
	cmd.AddCommand(NewWithdrawFeesTxCmd())
	cmd.AddCommand(NewChangeModeratorTxCmd())
	return cmd
}
