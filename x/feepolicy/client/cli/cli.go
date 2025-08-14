package cli

import (
	"fmt"

	"github.com/GPTx-global/guru-v2/x/feepolicy/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the cli query commands for the xmsquare module.
func GetQueryCmd() *cobra.Command {
	cexQueryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the xmsquare module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cexQueryCmd.AddCommand(
		GetCmdQueryModeratorAddress(),
		GetCmdQueryDiscount(),
		GetCmdQueryDiscounts(),
	)

	return cexQueryCmd
}

func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(NewChangeModeratorTxCmd())
	cmd.AddCommand(NewRegisterDiscountsTxCmd())
	cmd.AddCommand(NewRemoveDiscountsTxCmd())
	return cmd
}
