package cli

import (
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

// GetQueryCmd returns the parent command for all x/feeproxy CLI query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the feeproxy module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		GetParamsCmd(),
		GetModeratorAddressCmd(),
		GetAdminAddressCmd(),
		GetIsAdminCmd(),
		GetFeePercentageCmd(),
		GetReserveAddressCmd(),
	)

	return cmd
}

// GetParamsCmd queries the feeproxy params.
func GetParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Get the feeproxy params",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetModeratorAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "moderator-address",
		Short: "Get the feeproxy moderator address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

func GetAdminAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin-address",
		Short: "Get the feeproxy admin address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.AdminAddress(cmd.Context(), &types.QueryAdminAddressRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetIsAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "is-admin [address]",
		Short: "Query if the address is the feeproxy admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.IsAdmin(cmd.Context(), &types.QueryIsAdminRequest{Address: args[0]})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetFeePercentageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fee-percentage",
		Short: "Get the feeproxy fee percentage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.FeePercentage(cmd.Context(), &types.QueryFeePercentageRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetReserveAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reserve-address",
		Short: "Get the feeproxy reserve address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.ReserveAddress(cmd.Context(), &types.QueryReserveAddressRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
