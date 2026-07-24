package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

var (
	getClientQueryContext = client.GetClientQueryContext
	newQueryClient        = func(clientCtx client.Context) types.QueryClient { return types.NewQueryClient(clientCtx) }
	printQueryResponse    = func(clientCtx client.Context, response printableProto) error {
		return clientCtx.PrintProto(response)
	}
)

type printableProto interface {
	Reset()
	String() string
	ProtoMessage()
}

func GetCmdQueryModeratorAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "moderator_address",
		Short:   "Query the current Constitution moderator address",
		Example: fmt.Sprintf("%s query feepolicy moderator_address", version.AppName),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			response, err := newQueryClient(clientCtx).ModeratorAddress(
				cmd.Context(),
				&types.QueryModeratorAddressRequest{},
			)
			if err != nil {
				return err
			}
			return printQueryResponse(clientCtx, response)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryDiscount() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "discount [address]",
		Short:   "Query the discounts for an address",
		Example: fmt.Sprintf("%s query feepolicy discount [address]", version.AppName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			response, err := newQueryClient(clientCtx).Discount(
				cmd.Context(),
				&types.QueryDiscountRequest{Address: args[0]},
			)
			if err != nil {
				return err
			}
			return printQueryResponse(clientCtx, response)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryDiscounts() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "discounts",
		Short:   "Query registered discounts",
		Example: fmt.Sprintf("%s query feepolicy discounts", version.AppName),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageRequest, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}
			response, err := newQueryClient(clientCtx).Discounts(
				cmd.Context(),
				&types.QueryDiscountsRequest{Pagination: pageRequest},
			)
			if err != nil {
				return err
			}
			return printQueryResponse(clientCtx, response)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "discounts")
	return cmd
}
