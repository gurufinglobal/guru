package cli

import (
	"fmt"
	"strconv"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

// GetQueryCmd returns the root query command for the y/oracle module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		GetCmdQueryParams(),
		GetCmdQueryModeratorAddress(),
		GetCmdQueryOracleRequest(),
		GetCmdQueryOracleRequests(),
		GetCmdQueryOracleReports(),
		GetCmdQueryOracleResult(),
		GetCmdQueryOracleResults(),
		GetCmdQueryCategories(),
		GetCmdQueryWhitelist(),
	)

	return cmd
}

func GetCmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query the current oracle parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(&res.Params)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryModeratorAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "moderator-address",
		Short: "Query the oracle moderator address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.ModeratorAddress(cmd.Context(), &types.QueryModeratorAddressRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryOracleRequest() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request [request-id]",
		Short: "Query a single oracle request by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			requestID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRequest, "invalid request-id %q: %v", args[0], err)
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.OracleRequest(cmd.Context(), &types.QueryOracleRequestRequest{RequestId: requestID})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

const (
	flagCategory = "category"
	flagStatus   = "status"
)

func GetCmdQueryOracleRequests() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requests",
		Short: "Query oracle requests (optionally filtered by category and/or status)",
		Long: strings.TrimSpace(`Query oracle requests.

Filters:
- --category: CATEGORY_UNSPECIFIED | CATEGORY_OPERATION | CATEGORY_CRYPTO | CATEGORY_CURRENCY (or numeric value)
- --status:   STATUS_UNSPECIFIED | STATUS_ACTIVE | STATUS_INACTIVE (or numeric value)

If a filter is omitted or set to *_UNSPECIFIED, it is not applied.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			catStr, _ := cmd.Flags().GetString(flagCategory)
			statusStr, _ := cmd.Flags().GetString(flagStatus)

			category, err := ParseCategory(catStr)
			if err != nil {
				return err
			}
			status, err := ParseStatus(statusStr)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.OracleRequests(cmd.Context(), &types.QueryOracleRequestsRequest{
				Category: category,
				Status:   status,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String(flagCategory, "", "Category filter (enum name or numeric value); omit to not filter")
	cmd.Flags().String(flagStatus, "", "Status filter (enum name or numeric value); omit to not filter")

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

const (
	flagNonce    = "nonce"
	flagProvider = "provider"
)

func GetCmdQueryOracleReports() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reports [request-id]",
		Short: "Query reports submitted for an oracle request (with optional filters)",
		Long: strings.TrimSpace(`Query oracle reports for a request.

Examples:
- All reports for a request:
  oracle reports 1

- All reports for a specific nonce:
  oracle reports 1 --nonce 2

- Reports by a provider (across all nonces):
  oracle reports 1 --provider <provider-address>

- Single report lookup (provider + nonce):
  oracle reports 1 --nonce 2 --provider <provider-address>`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			requestID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRequest, "invalid request-id %q: %v", args[0], err)
			}

			nonce, err := cmd.Flags().GetUint64(flagNonce)
			if err != nil {
				return err
			}
			provider, err := cmd.Flags().GetString(flagProvider)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.OracleReports(cmd.Context(), &types.QueryOracleReportsRequest{
				RequestId: requestID,
				Nonce:     nonce,
				Provider:  provider,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Uint64(flagNonce, 0, "Nonce filter; 0 means 'all'/latest depending on query")
	cmd.Flags().String(flagProvider, "", "Provider filter (address string)")

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryOracleResult() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "result [request-id]",
		Short: "Query the aggregated oracle result (latest by default, or by nonce)",
		Long: strings.TrimSpace(`Query oracle result for a request.

By default, this queries the latest result. To query a specific nonce, set --nonce.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			requestID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRequest, "invalid request-id %q: %v", args[0], err)
			}

			nonce, err := cmd.Flags().GetUint64(flagNonce)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.OracleResult(cmd.Context(), &types.QueryOracleResultRequest{
				RequestId: requestID,
				Nonce:     nonce,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Uint64(flagNonce, 0, "Nonce to query; 0 queries the latest result")

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryOracleResults() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "results [request-id]",
		Short: "Query the result history for an oracle request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			requestID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRequest, "invalid request-id %q: %v", args[0], err)
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.OracleResults(cmd.Context(), &types.QueryOracleResultsRequest{
				RequestId: requestID,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryCategories() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "categories",
		Short: "Query enabled oracle categories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Categories(cmd.Context(), &types.QueryCategoriesRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryWhitelist() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whitelist",
		Short: "Query whitelisted provider addresses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Whitelist(cmd.Context(), &types.QueryWhitelistRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
