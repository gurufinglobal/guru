package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/spf13/cobra"
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
		NewUpdateParamsCmd(),
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

			requestDoc, err := parseRequestDocJSON(args[0])
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

			requestDoc, err := parseRequestDocJSON(args[0])
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

			requestIDStr := args[0]
			nonce := args[1]
			rawData := args[2]

			requestID, err := strconv.ParseUint(requestIDStr, 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "request id is not a valid uint64")
			}

			nonceUint64, err := strconv.ParseUint(nonce, 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "nonce is not a valid uint64")
			}

			msg := types.NewMsgSubmitOracleData(
				requestID,
				nonceUint64,
				rawData,
				clientCtx.GetFromAddress().String(),
				nil,
				clientCtx.GetFromAddress().String(),
			)

			dataset, err := msg.DataSet.Bytes()
			if err != nil {
				return err
			}

			signature, _, err := clientCtx.Keyring.Sign(
				clientCtx.FromName,
				dataset,
				signing.SignMode_SIGN_MODE_DIRECT,
			)
			if err != nil {
				return err
			}

			msg.DataSet.Signature = signature

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

func parseRequestDocJSON(path string) (*types.OracleRequestDoc, error) {
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

// NewUpdateParamsCmd implements the update oracle parameters command for governance proposals
func NewUpdateParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params [submit-window] [min-submit-per-window] [slash-fraction-downtime] [max-account-list-size]",
		Short: "Generate governance proposal to update oracle module parameters",
		Long: `Generate a governance proposal to update oracle module parameters.
This command creates a MsgUpdateParams that must be submitted through governance.

Example:
  # Create a governance proposal JSON file
  gurud tx oracle update-params 3600 1.0 0.01 1000 --generate-only > update_params_proposal.json

  # Submit the proposal through governance
  gurud tx gov submit-proposal update_params_proposal.json --from proposer`,
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Parse submit window
			submitWindow, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "submit window must be a valid uint64")
			}

			// Parse min submit per window
			minSubmitPerWindow, err := sdkmath.LegacyNewDecFromStr(args[1])
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "min submit per window must be a valid decimal")
			}

			// Parse slash fraction downtime
			slashFractionDowntime, err := sdkmath.LegacyNewDecFromStr(args[2])
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "slash fraction downtime must be a valid decimal")
			}

			// Parse max account list size
			maxAccountListSize, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "max account list size must be a valid uint64")
			}

			params := types.Params{
				EnableOracle:          true, // Always enabled
				SubmitWindow:          submitWindow,
				MinSubmitPerWindow:    minSubmitPerWindow,
				SlashFractionDowntime: slashFractionDowntime,
				MaxAccountListSize:    maxAccountListSize,
			}

			// Use governance module address as authority
			govModuleAddr := "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn" // This should be the governance module address

			msg := &types.MsgUpdateParams{
				Authority: govModuleAddr,
				Params:    params,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
