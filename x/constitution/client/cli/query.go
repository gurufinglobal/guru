package cli

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the root query command for the constitution module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        constitutiontypes.ModuleName,
		Short:                      "Querying commands for the constitution module",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdQueryParams(),
		CmdQueryBaseAddress(),
		CmdQueryModeratorAddress(),
		CmdQuerySeparationRatio(),
		CmdQueryMinGasPrice(),
	)

	return cmd
}

func CmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query the constitution parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := constitutiontypes.NewQueryClient(clientCtx)
			resp, err := queryClient.Params(cmd.Context(), &constitutiontypes.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryBaseAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "base-address",
		Short: "Query the constitution base address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := constitutiontypes.NewQueryClient(clientCtx)
			resp, err := queryClient.BaseAddress(cmd.Context(), &constitutiontypes.QueryBaseAddressRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryModeratorAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "moderator-address",
		Short: "Query the constitution moderator address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := constitutiontypes.NewQueryClient(clientCtx)
			resp, err := queryClient.ModeratorAddress(cmd.Context(), &constitutiontypes.QueryModeratorAddressRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQuerySeparationRatio() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "separation-ratio",
		Short: "Query the constitution separation ratio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := constitutiontypes.NewQueryClient(clientCtx)
			resp, err := queryClient.SeparationRatio(cmd.Context(), &constitutiontypes.QuerySeparationRatioRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryMinGasPrice() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "min-gas-price",
		Short: "Query the current and pending minimum gas price",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := constitutiontypes.NewQueryClient(clientCtx)
			resp, err := queryClient.MinGasPrice(cmd.Context(), &constitutiontypes.QueryMinGasPriceRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
