package cli

import (
	"fmt"
	"strconv"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/spf13/cobra"
)

const flagIncludeDeleted = "include-deleted"

var (
	getClientQueryContext = client.GetClientQueryContext
	newQueryClient        = bexv1.NewQueryClient
	printProto            = printClientProto
)

type printableProto interface {
	Reset()
	String() string
	ProtoMessage()
}

func printClientProto(clientCtx client.Context, msg printableProto) error {
	return clientCtx.PrintProto(msg)
}

func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the BEX module",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		CmdQueryExchange(),
		CmdQueryExchanges(),
		CmdQueryExchangesByExchangeAdmin(),
		CmdQueryIsBexAdmin(),
		CmdQueryReserveDepositors(),
		CmdQueryIsReserveDepositor(),
		CmdQueryCollectedFees(),
		CmdQueryLockedFees(),
		CmdQueryAvailableFees(),
		CmdQueryPendingLiabilities(),
		CmdQueryVolumeWindow(),
		CmdQueryQuote(),
	)
	return cmd
}

func CmdQueryReserveDepositors() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reserve-depositors [exchange-id]",
		Short: "Query BEX reserve depositors",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			pageReq, err := readPulsarPageRequest(cmd)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).ReserveDepositors(cmd.Context(), &bexv1.QueryReserveDepositorsRequest{
				ExchangeId: exchangeID,
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddPaginationFlagsToCmd(cmd, "bex reserve depositors")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryIsReserveDepositor() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "is-reserve-depositor [exchange-id] [depositor]",
		Short: "Query BEX reserve deposit permission",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).IsReserveDepositor(cmd.Context(), &bexv1.QueryIsReserveDepositorRequest{
				ExchangeId:       exchangeID,
				DepositorAddress: args[1],
			})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryExchange() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exchange [exchange-id]",
		Short: "Query a BEX exchange",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).Exchange(cmd.Context(), &bexv1.QueryExchangeRequest{ExchangeId: exchangeID})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryExchanges() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exchanges",
		Short: "Query BEX exchanges",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := readPulsarPageRequest(cmd)
			if err != nil {
				return err
			}
			includeDeleted, err := cmd.Flags().GetBool(flagIncludeDeleted)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).Exchanges(cmd.Context(), &bexv1.QueryExchangesRequest{
				Pagination:     pageReq,
				IncludeDeleted: includeDeleted,
			})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddPaginationFlagsToCmd(cmd, "bex exchanges")
	cmd.Flags().Bool(flagIncludeDeleted, false, "include deleted exchange tombstones")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryExchangesByExchangeAdmin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exchanges-by-exchange-admin [exchange-admin]",
		Short: "Query BEX exchanges by exchange admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := readPulsarPageRequest(cmd)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).ExchangesByExchangeAdmin(cmd.Context(), &bexv1.QueryExchangesByExchangeAdminRequest{
				ExchangeAdminAddress: args[0],
				Pagination:           pageReq,
			})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddPaginationFlagsToCmd(cmd, "bex exchanges by exchange admin")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryIsBexAdmin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "is-bex-admin [bex-admin]",
		Short: "Query BEX admin registration status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).IsBexAdmin(cmd.Context(), &bexv1.QueryIsBexAdminRequest{BexAdminAddress: args[0]})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryCollectedFees() *cobra.Command {
	return feeQueryCmd("collected-fees", "Query collected fees", func(c bexv1.QueryClient, ctx *cobra.Command, id uint64) (*bexv1.QueryFeesResponse, error) {
		return c.CollectedFees(ctx.Context(), &bexv1.QueryFeesRequest{ExchangeId: id})
	})
}

func CmdQueryLockedFees() *cobra.Command {
	return feeQueryCmd("locked-fees", "Query locked fees", func(c bexv1.QueryClient, ctx *cobra.Command, id uint64) (*bexv1.QueryFeesResponse, error) {
		return c.LockedFees(ctx.Context(), &bexv1.QueryFeesRequest{ExchangeId: id})
	})
}

func CmdQueryAvailableFees() *cobra.Command {
	return feeQueryCmd("available-fees", "Query available fees", func(c bexv1.QueryClient, ctx *cobra.Command, id uint64) (*bexv1.QueryFeesResponse, error) {
		return c.AvailableFees(ctx.Context(), &bexv1.QueryFeesRequest{ExchangeId: id})
	})
}

func CmdQueryPendingLiabilities() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pending-liabilities [exchange-id]",
		Short: "Query aggregate pending refund liabilities",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).PendingLiabilities(
				cmd.Context(),
				&bexv1.QueryPendingLiabilitiesRequest{ExchangeId: exchangeID},
			)
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func feeQueryCmd(use, short string, query func(bexv1.QueryClient, *cobra.Command, uint64) (*bexv1.QueryFeesResponse, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use + " [exchange-id]",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			resp, err := query(newQueryClient(clientCtx), cmd, exchangeID)
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryVolumeWindow() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume-window [exchange-id] [direction]",
		Short: "Query BEX volume window",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, direction, err := parseExchangeIDAndDirection(args[0], args[1])
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).VolumeWindow(cmd.Context(), &bexv1.QueryVolumeWindowRequest{ExchangeId: exchangeID, Direction: direction})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func CmdQueryQuote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quote [exchange-id] [input-denom] [amount-in]",
		Short: "Query BEX quote",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientQueryContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			resp, err := newQueryClient(clientCtx).QuoteSwap(cmd.Context(), &bexv1.QueryQuoteSwapRequest{
				ExchangeId: exchangeID,
				InputDenom: args[1],
				AmountIn:   args[2],
			})
			if err != nil {
				return err
			}
			return printProto(clientCtx, resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func parseExchangeIDAndDirection(idRaw, directionRaw string) (uint64, bexv1.SwapDirection, error) {
	exchangeID, err := strconv.ParseUint(idRaw, 10, 64)
	if err != nil {
		return 0, bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, err
	}
	switch directionRaw {
	case "a-to-b", "A_TO_B", "1":
		return exchangeID, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, nil
	case "b-to-a", "B_TO_A", "2":
		return exchangeID, bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A, nil
	default:
		return 0, bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, fmt.Errorf("invalid direction %q", directionRaw)
	}
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
