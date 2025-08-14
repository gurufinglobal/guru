package cli

import (
	"io/ioutil"

	errorsmod "cosmossdk.io/errors"
	"github.com/GPTx-global/guru-v2/x/feepolicy/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

func NewChangeModeratorTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change_moderator [new_moderator_address] --from [moderator_address]",
		Short: "Change the moderator address for the cex module",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			newModAddr, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgChangeModerator(clientCtx.GetFromAddress(), newModAddr)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewRegisterDiscountsTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register_discounts [path_to_json] --from [moderator_address]",
		Short: "Register discounts from json file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			cdc := codec.NewProtoCodec(clientCtx.InterfaceRegistry)

			var discounts types.AccountDiscounts

			if err := cdc.UnmarshalJSON([]byte(args[0]), &discounts); err != nil {
				// If that fails, treat it as a filepath
				contents, err := ioutil.ReadFile(args[0])
				if err != nil {
					return errorsmod.Wrapf(types.ErrInvalidJsonFile, "%s", err)
				}

				if err := cdc.UnmarshalJSON(contents, &discounts); err != nil {
					return errorsmod.Wrapf(types.ErrInvalidJsonFile, "%s", err)
				}

			}

			msg := types.NewMsgRegisterDiscounts(clientCtx.GetFromAddress(), discounts.Discounts)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewRemoveDiscountsTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove_discounts [discount_address] [module] [msg_type] --from [moderator_address]",
		Short: "Remove discounts for the given address, module and msg type",
		Args:  cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			discountAddr, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgRemoveDiscounts(clientCtx.GetFromAddress(), discountAddr, "", "")
			if len(args) == 2 {
				msg = types.NewMsgRemoveDiscounts(clientCtx.GetFromAddress(), discountAddr, args[1], "")
			} else if len(args) == 3 {
				msg = types.NewMsgRemoveDiscounts(clientCtx.GetFromAddress(), discountAddr, args[1], args[2])
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
