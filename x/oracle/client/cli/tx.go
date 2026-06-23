package cli

import (
	"fmt"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the root transaction command for the oracle module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Transaction commands for the oracle module",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdUpdateParams(),
		CmdUpsertTask(),
		CmdRemoveTask(),
	)

	return cmd
}

func CmdUpdateParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params [min-validators] [min-sources] [history-limit]",
		Short: "Update oracle parameters",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg, err := newMsgUpdateParams(clientCtx.GetFromAddress().String(), args)
			if err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdUpsertTask() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upsert-task [symbol] [submission-interval]",
		Short: "Add or update a numeric oracle task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			enabled, err := cmd.Flags().GetBool(flagEnabled)
			if err != nil {
				return err
			}

			msg, err := newMsgUpsertTask(clientCtx.GetFromAddress().String(), args[0], args[1], enabled)
			if err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().Bool(flagEnabled, true, "enable the oracle task")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdRemoveTask() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-task [symbol]",
		Short: "Remove an oracle task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &oraclev1.MsgRemoveTask{
				Moderator: clientCtx.GetFromAddress().String(),
				Symbol:    args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func newMsgUpdateParams(moderator string, args []string) (*oraclev1.MsgUpdateParams, error) {
	minValidators, err := parsePositiveUint32Argument("min_validators", args[0])
	if err != nil {
		return nil, err
	}
	minSources, err := parsePositiveUint32Argument("min_sources", args[1])
	if err != nil {
		return nil, err
	}
	historyLimit, err := parsePositiveUint32Argument("history_limit", args[2])
	if err != nil {
		return nil, err
	}

	return &oraclev1.MsgUpdateParams{
		Moderator: moderator,
		Params: &oraclev1.Params{
			MinValidators: minValidators,
			MinSources:    minSources,
			HistoryLimit:  historyLimit,
		},
	}, nil
}

func newMsgUpsertTask(moderator, symbol, intervalRaw string, enabled bool) (*oraclev1.MsgUpsertTask, error) {
	interval, err := parsePositiveUint32Argument("submission_interval", intervalRaw)
	if err != nil {
		return nil, err
	}

	return &oraclev1.MsgUpsertTask{
		Moderator: moderator,
		Task: &oraclev1.OracleTask{
			Symbol:             symbol,
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            enabled,
			SubmissionInterval: interval,
		},
	}, nil
}

func parsePositiveUint32Argument(fieldName, raw string) (uint32, error) {
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	if value == 0 {
		return 0, fmt.Errorf("invalid %s: must be positive", fieldName)
	}

	return uint32(value), nil
}

const flagEnabled = "enabled"
