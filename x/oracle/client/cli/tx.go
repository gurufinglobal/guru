package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"

	"github.com/GPTx-global/guru-v2/x/oracle/types"
	errorsmod "cosmossdk.io/errors"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewRegisterOracleRequestDocCmd(),
		NewUpdateOracleRequestDocCmd(),
		NewSubmitOracleDataCmd(),
		NewUpdateModeratorAddressCmd(),
	)

	return cmd
}

// NewRegisterOracleRequestDocCmd implements the register oracle request document command
func NewRegisterOracleRequestDocCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-request [path/to/request-doc.json]",
		Short: "Register a new oracle request document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			requestDoc, err := parseRequestDocJson(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgRegisterOracleRequestDoc(
				clientCtx.GetFromAddress().String(),
				*requestDoc,
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpdateOracleRequestDocCmd implements the update oracle request document command
func NewUpdateOracleRequestDocCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-request [path/to/request-doc.json] [reason]",
		Short: "Update an existing oracle request document",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			requestDoc, err := parseRequestDocJson(args[0])
			if err != nil {
				return err
			}

			reason := args[1]

			msg := types.NewMsgUpdateOracleRequestDoc(
				clientCtx.GetFromAddress().String(),
				*requestDoc,
				reason,
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSubmitOracleDataCmd implements the submit oracle data command
func NewSubmitOracleDataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-data [request-id] [nonce] [raw-data]",
		Short: "Submit oracle data for a request",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			requestId := args[0]
			nonce := args[1]
			rawData := args[2]

			requestIdUint64, err := strconv.ParseUint(requestId, 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "request id is not a valid uint64")
			}

			nonceUint64, err := strconv.ParseUint(nonce, 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "nonce is not a valid uint64")
			}

			msg := types.NewMsgSubmitOracleData(
				requestIdUint64,
				nonceUint64,
				rawData,
				clientCtx.GetFromAddress().String(),
				"NOT USED", // signature will be added by the client
				clientCtx.GetFromAddress().String(),
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewUpdateModeratorAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-moderator-address [moderator-address]",
		Short: "Update the moderator address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgUpdateModeratorAddress(
				clientCtx.GetFromAddress().String(),
				args[0],
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parseRequestDocJson(path string) (*types.OracleRequestDoc, error) {
	var doc types.OracleRequestDoc

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(contents, &doc)
	if err != nil {
		return nil, err
	}

	return &doc, nil
}
