package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
	"github.com/spf13/cobra"
)

// GetCmdQueryModeratorAddress returns the cli command for querying the moderator address.
func GetCmdQueryModeratorAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "moderator_address",
		Short:   "Query the current moderator address",
		Example: fmt.Sprintf("%s query feepolicy moderator_address", version.AppName),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryModeratorAddressRequest{}
			res, err := queryClient.ModeratorAddress(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryDiscount returns the cli command for querying the discounts for an address.
func GetCmdQueryDiscount() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "discount [address]",
		Short:   "Query the discounts for an address",
		Example: fmt.Sprintf("%s query feepolicy discount [address]", version.AppName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryDiscountRequest{Address: args[0]}
			res, err := queryClient.Discount(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryDiscounts returns the cli command for querying the discounts with pagination.
func GetCmdQueryDiscounts() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "discounts",
		Short:   "Query the discounts with pagination",
		Long:    "Query the discounts with pagination",
		Example: fmt.Sprintf("%s query feepolicy discounts", version.AppName),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			req := &types.QueryDiscountsRequest{
				Pagination: pageReq,
			}

			res, err := queryClient.Discounts(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "discounts")

	return cmd
}
