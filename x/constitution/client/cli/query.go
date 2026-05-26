package cli

import (
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
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

			queryClient := constitutionv1.NewQueryClient(clientCtx)
			resp, err := queryClient.Params(cmd.Context(), &constitutionv1.QueryParamsRequest{})
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

			queryClient := constitutionv1.NewQueryClient(clientCtx)
			resp, err := queryClient.BaseAddress(cmd.Context(), &constitutionv1.QueryBaseAddressRequest{})
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

			queryClient := constitutionv1.NewQueryClient(clientCtx)
			resp, err := queryClient.ModeratorAddress(cmd.Context(), &constitutionv1.QueryModeratorAddressRequest{})
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

			queryClient := constitutionv1.NewQueryClient(clientCtx)
			resp, err := queryClient.SeparationRatio(cmd.Context(), &constitutionv1.QuerySeparationRatioRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
