package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	querytypes "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/version"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const (
	flagRefundStatus   = "status"
	flagRefundReceiver = "receiver"
)

// GetCmdQueryParams returns the effective refund retry governance parameters.
func GetCmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query TransSwap refund retry parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Params(
				cmd.Context(),
				&types.QueryParamsRequest{},
			)
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryRefund returns one stable refund record by ID.
func GetCmdQueryRefund() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refund [refund-id]",
		Short: "Query one TransSwap refund record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Refund(
				cmd.Context(),
				&types.QueryRefundRequest{RefundId: args[0]},
			)
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryRefunds lists refund records with optional state and receiver
// filters. Pagination is evaluated after filtering on the server.
func GetCmdQueryRefunds() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refunds",
		Short: "List TransSwap refund records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			statusRaw, err := cmd.Flags().GetString(flagRefundStatus)
			if err != nil {
				return err
			}
			refundStatus, err := parseRefundStatus(statusRaw)
			if err != nil {
				return err
			}
			receiver, err := cmd.Flags().GetString(flagRefundReceiver)
			if err != nil {
				return err
			}
			pageReq, err := readPageRequest(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Refunds(
				cmd.Context(),
				&types.QueryRefundsRequest{
					Status:     refundStatus,
					Receiver:   receiver,
					Pagination: pageReq,
				},
			)
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagRefundStatus, "", "filter by refund status (for example pending or manual-claimable; all disables the filter)")
	cmd.Flags().String(flagRefundReceiver, "", "filter by exact cross-chain receiver")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "refund records")
	return cmd
}

func parseRefundStatus(raw string) (types.RefundStatus, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "all") {
		return types.RefundStatus_REFUND_STATUS_UNSPECIFIED, nil
	}
	if numeric, err := strconv.ParseInt(raw, 10, 32); err == nil {
		refundStatus := types.RefundStatus(numeric)
		if _, ok := types.RefundStatus_name[int32(refundStatus)]; ok {
			return refundStatus, nil
		}
		return types.RefundStatus_REFUND_STATUS_UNSPECIFIED, fmt.Errorf("unsupported refund status %q", raw)
	}

	normalized := strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(raw))
	if !strings.HasPrefix(normalized, "REFUND_STATUS_") {
		normalized = "REFUND_STATUS_" + normalized
	}
	value, ok := types.RefundStatus_value[normalized]
	if !ok {
		return types.RefundStatus_REFUND_STATUS_UNSPECIFIED, fmt.Errorf("unsupported refund status %q", raw)
	}
	return types.RefundStatus(value), nil
}

// GetCmdQueryDenom defines the command to query a denomination from a given hash or ibc denom.
func GetCmdQueryDenom() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "denom [hash/denom]",
		Short:   "Query the denom trace info from a given hash or ibc denom",
		Long:    "Query the denom trace info from a given hash or ibc denom",
		Example: fmt.Sprintf("%s query ibc-transwap denom 27A6394C3F9FF9C9DCF5DFFADF9BB5FE9A37C7E92B006199894CF1824DF9AC7C", version.AppName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryDenomRequest{
				Hash: args[0],
			}

			res, err := queryClient.Denom(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryDenoms defines the command to query all the denominations that this chain maintains.
func GetCmdQueryDenoms() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "denoms",
		Short:   "Query for all token denominations",
		Long:    "Query for all token denominations",
		Example: fmt.Sprintf("%s query ibc-transwap denoms", version.AppName),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			pageReq, err := readPageRequest(cmd)
			if err != nil {
				return err
			}

			req := &types.QueryDenomsRequest{
				Pagination: pageReq,
			}

			res, err := queryClient.Denoms(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "denominations")

	return cmd
}

func readPageRequest(cmd *cobra.Command) (*querytypes.PageRequest, error) {
	flagSet, err := client.FlagSetWithPageKeyDecoded(cmd.Flags())
	if err != nil {
		return nil, err
	}
	return client.ReadPageRequest(flagSet)
}

// GetCmdQueryEscrowAddress returns the command handler for transwap escrow address querying.
func GetCmdQueryEscrowAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "escrow-address",
		Short:   "Get the escrow address for a channel",
		Long:    "Get the escrow address for a channel",
		Args:    cobra.ExactArgs(2),
		Example: fmt.Sprintf("%s query ibc-transwap escrow-address [port] [channel-id]", version.AppName),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			port := args[0]
			channel := args[1]
			addr := types.GetEscrowAddress(port, channel)
			return clientCtx.PrintString(fmt.Sprintf("%s\n", addr.String()))
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// GetCmdQueryDenomHash defines the command to query a denomination hash from a given trace.
func GetCmdQueryDenomHash() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "denom-hash [trace]",
		Short:   "Query the denom hash info from a given denom trace",
		Long:    "Query the denom hash info from a given denom trace",
		Example: fmt.Sprintf("%s query ibc-transwap denom-hash transwap/channel-0/uatom", version.AppName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryDenomHashRequest{
				Trace: args[0],
			}

			res, err := queryClient.DenomHash(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryTotalEscrowForDenom defines the command to query the total amount of tokens in escrow for a denom
func GetCmdQueryTotalEscrowForDenom() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "total-escrow [denom]",
		Short:   "Query the total amount of tokens in escrow for a denom",
		Long:    "Query the total amount of tokens in escrow for a denom",
		Example: fmt.Sprintf("%s query ibc-transwap total-escrow uosmo", version.AppName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryTotalEscrowForDenomRequest{
				Denom: args[0],
			}

			res, err := queryClient.TotalEscrowForDenom(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
