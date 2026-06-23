package cli

import (
	"fmt"
	"strconv"

	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the root transaction command for the constitution module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        constitutiontypes.ModuleName,
		Short:                      "Transactions commands for the constitution module",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdUpdateBaseAddress(),
		CmdUpdateModeratorAddress(),
		CmdUpdateSeparationRatio(),
	)

	return cmd
}

func CmdUpdateBaseAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-base-address [base-address]",
		Short: "Update constitution base address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &constitutionv1.MsgUpdateBaseAddress{
				Moderator:   clientCtx.GetFromAddress().String(),
				BaseAddress: args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdUpdateModeratorAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-moderator-address [moderator-address]",
		Short: "Update constitution moderator address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &constitutionv1.MsgUpdateModeratorAddress{
				Moderator:        clientCtx.GetFromAddress().String(),
				ModeratorAddress: args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdUpdateSeparationRatio() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-separation-ratio [base-ppm] [burn-ppm] [validators-ppm]",
		Short: "Update constitution separation ratio",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			basePpm, err := parsePPMArgument("base_ppm", args[0])
			if err != nil {
				return err
			}
			burnPpm, err := parsePPMArgument("burn_ppm", args[1])
			if err != nil {
				return err
			}
			validatorsPpm, err := parsePPMArgument("validators_ppm", args[2])
			if err != nil {
				return err
			}

			msg := &constitutionv1.MsgUpdateSeparationRatio{
				Moderator: clientCtx.GetFromAddress().String(),
				SeparationRatio: &constitutionv1.SeparationRatio{
					BasePpm:       basePpm,
					BurnPpm:       burnPpm,
					ValidatorsPpm: validatorsPpm,
				},
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parsePPMArgument(fieldName, raw string) (uint32, error) {
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", fieldName, err)
	}

	return uint32(value), nil
}
