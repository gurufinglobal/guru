package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

const globalPolicySelector = "global"

func parsePolicyAddress(raw string) (string, bool) {
	if raw == globalPolicySelector {
		return "", true
	}
	return raw, false
}

func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		GetCmdQueryModeratorAddress(),
		GetCmdQueryDiscount(),
		GetCmdQueryDiscounts(),
	)
	return cmd
}

func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transaction subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		NewChangeModeratorTxCmd(),
		NewRegisterDiscountsTxCmd(),
		NewRemoveDiscountsTxCmd(),
	)
	return cmd
}
