package cli

import (
	"fmt"
	"strconv"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

// GetTxCmd returns the transaction commands for the y/oracle module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewRegisterRequestCmd(),
		NewUpdateRequestCmd(),
		NewUpdateModeratorAddressCmd(),
		NewAddToWhitelistCmd(),
		NewRemoveFromWhitelistCmd(),
	)

	return cmd
}

// <appd> tx oracle register-request <category> <symbol> <count> <period> --flags
func NewRegisterRequestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-request [category] [symbol] [count] [period]",
		Short: "Register a new oracle request (moderator only)",
		Long: strings.TrimSpace(`Register a new oracle request.

category can be an enum name (e.g. CATEGORY_CRYPTO) or numeric value (e.g. 2).

count is the remaining number of executions (period transitions). It is decremented each period; when it reaches 0, the request becomes inactive.`),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			category, err := ParseCategory(args[0])
			if err != nil {
				return err
			}

			symbol := args[1]

			count, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "count is not a valid uint64")
			}

			period, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "period is not a valid uint64")
			}

			msg := &types.MsgRegisterOracleRequest{
				ModeratorAddress: clientCtx.GetFromAddress().String(),
				Category:         category,
				Symbol:           symbol,
				Count:            count,
				Period:           period,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// <appd> tx oracle update-request <request-id> <count> <period> <status> --flags
func NewUpdateRequestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-request [request-id] [count] [period] [status]",
		Short: "Update an existing oracle request (moderator only)",
		Long: strings.TrimSpace(`Update an existing oracle request.

Notes:
- count:  0 to skip updating count (remaining executions)
- period: 0 to skip updating period
- status: STATUS_UNSPECIFIED (or 0) to skip updating status

status can be an enum name (e.g. STATUS_ACTIVE) or numeric value (e.g. 1).`),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			requestID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "request-id is not a valid uint64")
			}

			count, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "count is not a valid uint64")
			}

			period, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "period is not a valid uint64")
			}

			status, err := ParseStatus(args[3])
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateOracleRequest{
				ModeratorAddress: clientCtx.GetFromAddress().String(),
				RequestId:        requestID,
				Count:            count,
				Period:           period,
				Status:           status,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewUpdateModeratorAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-moderator-address [new-moderator-address]",
		Short: "Update the oracle moderator address (moderator only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateModeratorAddress{
				ModeratorAddress:    clientCtx.GetFromAddress().String(),
				NewModeratorAddress: args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewAddToWhitelistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whitelist-add [address]",
		Short: "Add an address to the oracle whitelist (moderator only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgAddToWhitelist{
				ModeratorAddress: clientCtx.GetFromAddress().String(),
				Address:          args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewRemoveFromWhitelistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whitelist-remove [address]",
		Short: "Remove an address from the oracle whitelist (moderator only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgRemoveFromWhitelist{
				ModeratorAddress: clientCtx.GetFromAddress().String(),
				Address:          args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
