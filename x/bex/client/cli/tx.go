package cli

import (
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/gurufinglobal/guru/v2/x/bex/types"
)

func NewRegisterAdminTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-admin [admin_address] [exchane_id] --from [moderator_address]",
		Short: "Register a new admin or reset admin for exchange with given id",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			newAdminAddr, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			exchangeID := math.NewInt(0)
			if len(args) > 1 {
				var ok bool
				exchangeID, ok = math.NewIntFromString(args[1])
				if !ok {
					return errorsmod.Wrapf(types.ErrInvalidExchange, " invalid id")
				}
			}

			msg := types.NewMsgRegisterAdmin(clientCtx.GetFromAddress(), newAdminAddr, exchangeID)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewRemoveAdminTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-admin [admin_address] --from [moderator_address]",
		Short: "remove the admin_address from admin list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			adminAddr, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgRemoveAdmin(clientCtx.GetFromAddress(), adminAddr)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewRegisterExchangeTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-exchange [path_to_json] --from [admin_address]",
		Short: "Register a new exchange from json file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			cdc := codec.NewProtoCodec(clientCtx.InterfaceRegistry)

			var exchange types.Exchange

			if err := cdc.UnmarshalJSON([]byte(args[0]), &exchange); err != nil {
				// If that fails, treat it as a filepath
				contents, err := os.ReadFile(args[0])
				if err != nil {
					return errorsmod.Wrapf(types.ErrInvalidJSONFile, "%s", err)
				}

				if err := cdc.UnmarshalJSON(contents, &exchange); err != nil {
					return errorsmod.Wrapf(types.ErrInvalidJSONFile, "%s", err)
				}
				exchange.AdminAddress = clientCtx.GetFromAddress().String()
			}

			msg := types.NewMsgRegisterExchange(clientCtx.GetFromAddress(), &exchange)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewUpdateExchangeTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-exchange [id] [key] [value] --from [admin_address]",
		Short: "Update the exchange attribute by given id",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			id, ok := math.NewIntFromString(args[0])
			if !ok {
				return errorsmod.Wrapf(types.ErrInvalidExchange, " invalid id")
			}

			msg := types.NewMsgUpdateExchange(clientCtx.GetFromAddress(), id, args[1], args[2])

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewUpdateRatemeterTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-ratemeter [request_count_limit] [request_period] --from [moderator_address]",
		Short: "Update the ratemeter. request_period example: 10s, 1m, 1h, 1h30m. 24h",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			requestCountLimit, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRatemeter, " %s", err)
			}

			requestPeriod, err := time.ParseDuration(args[1])
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRatemeter, " %s", err)
			}

			msg := types.NewMsgUpdateRatemeter(clientCtx.GetFromAddress(), &types.Ratemeter{RequestCountLimit: requestCountLimit, RequestPeriod: requestPeriod})

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewWithdrawFeesTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw-fees [exchange_id] [withdraw_address] --from [admin_address]",
		Short: "Withdraw the fees from the exchange",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			exchangeID, ok := math.NewIntFromString(args[0])
			if !ok {
				return errorsmod.Wrapf(types.ErrInvalidExchange, " invalid id")
			}

			withdrawAddress, err := sdk.AccAddressFromBech32(args[1])
			if err != nil {
				return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " %s", err)
			}

			msg := types.NewMsgWithdrawFees(clientCtx.GetFromAddress(), exchangeID, withdrawAddress)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewChangeModeratorTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change-moderator [new_moderator_address] --from [moderator_address]",
		Short: "Change the moderator address",
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
