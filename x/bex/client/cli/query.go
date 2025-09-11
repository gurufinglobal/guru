package cli

import (
	"github.com/GPTx-global/guru-v2/x/bex/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"
)

func GetCmdQueryModeratorAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "moderator-address",
		Short: "Query the current moderator address",
		Args:  cobra.NoArgs,
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

func GetCmdQueryExchanges() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exchanges",
		Short: "Query the list of all exchanges or one exchange by given id",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryExchangesRequest{}

			if len(args) > 0 {
				req.Id = args[0]
			}

			res, err := queryClient.Exchanges(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryIsAdmin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "is-admin [address]",
		Short: "Query if the address is an admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryIsAdminRequest{Address: args[0]}

			res, err := queryClient.IsAdmin(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryNextExchangeId() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "next-exchange-id",
		Short: "Query the exchange id for registering new exchange",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryNextExchangeIdRequest{}
			res, err := queryClient.NextExchangeId(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func GetCmdQueryRatemeter() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ratemeter",
		Short: "Query the ratemeter",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryRatemeterRequest{}
			res, err := queryClient.Ratemeter(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
