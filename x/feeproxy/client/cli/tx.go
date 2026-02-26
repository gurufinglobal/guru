package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

// GetTxCmd returns the parent command for all x/feeproxy CLI tx commands.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "feeproxy transactions subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewRegisterAdminTxCmd(),
		NewUpdateFeePercentageTxCmd(),
		NewUpdateReserveAddressTxCmd(),
	)

	return cmd
}

func NewRegisterAdminTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-admin [admin_address] --from [moderator_address]",
		Short: "Register (or replace) the feeproxy admin (moderator only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgRegisterAdmin{
				ModeratorAddress: clientCtx.GetFromAddress().String(),
				AdminAddress:     args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewUpdateFeePercentageTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-fee-percentage [fee_percentage] --from [admin_address]",
		Short: "Update the feeproxy fee percentage (admin only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			feePct, err := sdkmath.LegacyNewDecFromStr(args[0])
			if err != nil {
				return fmt.Errorf("invalid fee_percentage: %w", err)
			}

			msg := &types.MsgUpdateFeePercentage{
				AdminAddress:  clientCtx.GetFromAddress().String(),
				FeePercentage: feePct,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewUpdateReserveAddressTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-reserve-address [reserve_address] --from [admin_address]",
		Short: "Update the feeproxy reserve address (admin only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateReserveAddress{
				AdminAddress:   clientCtx.GetFromAddress().String(),
				ReserveAddress: args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
