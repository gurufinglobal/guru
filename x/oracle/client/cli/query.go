package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	errorsmod "cosmossdk.io/errors"
	"github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/cosmos/cosmos-sdk/version"
)

// GetQueryCmd returns the cli query commands for this module
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
		GetCmdQueryOracleRequestDoc(),
		GetCmdQueryOracleData(),
		GetCmdQueryOracleSubmitData(),
		GetCmdQueryOracleRequestDocs(),
		GetCmdQueryModeratorAddress(),
	)

	return cmd
}

// GetCmdQueryParams implements the params query command
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

// GetCmdQueryOracleRequestDoc implements the oracle request document query command
func GetCmdQueryOracleRequestDoc() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request-doc [request-id]",
		Short: "Query an oracle request document by Request ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			requestId, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRequestId, "args[0] parse error: %s", args[0])
			}

			res, err := queryClient.OracleRequestDoc(cmd.Context(), &types.QueryOracleRequestDocRequest{
				RequestId: requestId,
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

// GetCmdQueryOracleData implements the oracle data query command
func GetCmdQueryOracleData() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data [request-id]",
		Short: "Query an oracle data by Request ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			requestId, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRequestId, "args[0] parse error: %s", args[0])
			}

			res, err := queryClient.OracleData(cmd.Context(), &types.QueryOracleDataRequest{
				RequestId: requestId,
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

// GetCmdQueryOracleSubmitData implements the oracle data query command
func GetCmdQueryOracleSubmitData() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-data [request-id] [nonce] [provider-account]",
		Short: "Query oracle submit data for a request",
		Long: strings.TrimSpace(fmt.Sprintf(`Query oracle submit data for a request.

Example:
$ %s query oracle submit-data 1 1
$ %s query oracle submit-data 1 1 provider-account

Description:
- By default, shows all submissions for the given [request-id] and [nonce]
- Optional [provider-account] parameter filters results to show only submissions from that account`,
			version.AppName, version.AppName)),
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			requestId, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errorsmod.Wrapf(types.ErrInvalidRequestId, "args[0] parse error: %s", args[0])
			}

			nonce, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid nonce: %w", err)
			}

			provider := ""
			if len(args) > 2 {
				provider = args[2]
			}

			res, err := queryClient.OracleSubmitData(cmd.Context(), &types.QueryOracleSubmitDataRequest{
				RequestId: requestId,
				Nonce:     nonce,
				Provider:  provider,
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

// GetCmdQueryOracleRequestDocs implements the oracle request documents query command
func GetCmdQueryOracleRequestDocs() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request-docs [status]",
		Short: "Query all oracle request documents",
		// Args:  cobra.MaximumNArgs(1),
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			var status int64 = 0
			if len(args) > 0 {
				status, err = strconv.ParseInt(args[0], 10, 32)
				if err != nil {
					return fmt.Errorf("invalid status: %w", err)
				}
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.OracleRequestDocs(cmd.Context(), &types.QueryOracleRequestDocsRequest{
				Status: types.RequestStatus(status),
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

// GetCmdQueryModeratorAddress implements the moderator address query command
func GetCmdQueryModeratorAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "moderator-address",
		Short: "Query the moderator address",
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
