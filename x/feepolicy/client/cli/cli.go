package cli

import (
	"fmt"

	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
)

// GetQueryCmd returns the cli query commands for the feepolicy module.
func GetQueryCmd() *cobra.Command {
	cexQueryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
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

// GetTxCmd returns the cli transaction commands for the feepolicy module.
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
