package cli

import (
	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the root query command for the oracle module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the oracle module",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdQueryParams(),
		CmdQueryActiveTasks(),
		CmdQueryTask(),
		CmdQueryLatestValue(),
		CmdQueryLatestValues(),
		CmdQueryHistory(),
	)

	return cmd
}

func CmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query oracle parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := oraclev1.NewQueryClient(clientCtx)
			resp, err := queryClient.Params(cmd.Context(), &oraclev1.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryActiveTasks() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "active-tasks",
		Short: "Query active oracle tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := readPulsarPageRequest(cmd)
			if err != nil {
				return err
			}

			queryClient := oraclev1.NewQueryClient(clientCtx)
			resp, err := queryClient.ActiveTasks(cmd.Context(), &oraclev1.QueryActiveTasksRequest{Pagination: pageReq})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "oracle tasks")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryTask() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task [symbol]",
		Short: "Query an oracle task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := oraclev1.NewQueryClient(clientCtx)
			resp, err := queryClient.Task(cmd.Context(), &oraclev1.QueryTaskRequest{Symbol: args[0]})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryLatestValue() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "latest-value [symbol]",
		Short: "Query the latest accepted oracle value for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := oraclev1.NewQueryClient(clientCtx)
			resp, err := queryClient.LatestValue(cmd.Context(), &oraclev1.QueryLatestValueRequest{Symbol: args[0]})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryLatestValues() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "latest-values",
		Short: "Query latest accepted oracle values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := readPulsarPageRequest(cmd)
			if err != nil {
				return err
			}

			queryClient := oraclev1.NewQueryClient(clientCtx)
			resp, err := queryClient.LatestValues(cmd.Context(), &oraclev1.QueryLatestValuesRequest{Pagination: pageReq})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "oracle values")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryHistory() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history [symbol]",
		Short: "Query bounded oracle value history for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := readPulsarPageRequest(cmd)
			if err != nil {
				return err
			}

			queryClient := oraclev1.NewQueryClient(clientCtx)
			resp, err := queryClient.History(cmd.Context(), &oraclev1.QueryHistoryRequest{
				Symbol:     args[0],
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "oracle history")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func readPulsarPageRequest(cmd *cobra.Command) (*queryv1beta1.PageRequest, error) {
	flagSet, err := client.FlagSetWithPageKeyDecoded(cmd.Flags())
	if err != nil {
		return nil, err
	}
	pageReq, err := client.ReadPageRequest(flagSet)
	if err != nil {
		return nil, err
	}

	return &queryv1beta1.PageRequest{
		Key:        pageReq.GetKey(),
		Offset:     pageReq.GetOffset(),
		Limit:      pageReq.GetLimit(),
		CountTotal: pageReq.GetCountTotal(),
		Reverse:    pageReq.GetReverse(),
	}, nil
}
