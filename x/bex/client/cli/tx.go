package cli

import (
	"strconv"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	getClientTxContext       = client.GetClientTxContext
	generateOrBroadcastTxCLI = tx.GenerateOrBroadcastTxCLI
)

func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Transaction commands for the BEX module",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		CmdRegisterAdmin(),
		CmdUpdateAdmin(),
		CmdRemoveAdmin(),
		CmdRegisterExchange(),
		CmdUpdateExchange(),
		CmdDeleteExchange(),
		CmdAddReserveDepositor(),
		CmdRemoveReserveDepositor(),
		CmdDepositReserve(),
		CmdWithdrawReserve(),
		CmdWithdrawFees(),
	)
	return cmd
}

func CmdAddReserveDepositor() *cobra.Command {
	return reserveDepositorCmd(
		"add-reserve-depositor [exchange-id] [depositor]",
		"Allow an address to deposit into a BEX reserve",
		func(admin string, exchangeID uint64, depositor string) sdk.Msg {
			return &bexv1.MsgAddReserveDepositor{
				AdminAddress:     admin,
				ExchangeId:       exchangeID,
				DepositorAddress: depositor,
			}
		},
	)
}

func CmdRemoveReserveDepositor() *cobra.Command {
	return reserveDepositorCmd(
		"remove-reserve-depositor [exchange-id] [depositor]",
		"Revoke an address's BEX reserve deposit permission",
		func(admin string, exchangeID uint64, depositor string) sdk.Msg {
			return &bexv1.MsgRemoveReserveDepositor{
				AdminAddress:     admin,
				ExchangeId:       exchangeID,
				DepositorAddress: depositor,
			}
		},
	)
}

func reserveDepositorCmd(use, short string, build func(string, uint64, string) sdk.Msg) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(
				clientCtx,
				cmd.Flags(),
				build(clientCtx.GetFromAddress().String(), exchangeID, args[1]),
			)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdRegisterAdmin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-admin [admin]",
		Short: "Register a BEX admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgRegisterAdmin{
				Moderator:    clientCtx.GetFromAddress().String(),
				AdminAddress: args[0],
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdRemoveAdmin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-admin [admin]",
		Short: "Remove a BEX admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgRemoveAdmin{
				Moderator:    clientCtx.GetFromAddress().String(),
				AdminAddress: args[0],
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdUpdateAdmin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-admin [old-admin] [new-admin]",
		Short: "Replace one BEX exchange registrar",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgUpdateAdmin{
				Moderator:       clientCtx.GetFromAddress().String(),
				OldAdminAddress: args[0],
				NewAdminAddress: args[1],
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdRegisterExchange() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-exchange [spec-json]",
		Short: "Register a BEX exchange",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := &bexv1.MsgRegisterExchange{}
			if err := decodeStrictJSON(args[0], msg); err != nil {
				return err
			}
			msg.BexAdminAddress = clientCtx.GetFromAddress().String()
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdUpdateExchange() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-exchange [exchange-id] [patch-json] [expected-revision]",
		Short: "Update a BEX exchange",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			expectedRevision, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return err
			}
			patch := &bexv1.ExchangeUpdatePatch{}
			if err := decodeStrictJSON(args[1], patch); err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgUpdateExchange{
				AdminAddress:     clientCtx.GetFromAddress().String(),
				ExchangeId:       exchangeID,
				Patch:            patch,
				ExpectedRevision: expectedRevision,
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdDeleteExchange() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete-exchange [exchange-id]",
		Short: "Delete a BEX exchange",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgDeleteExchange{
				AdminAddress: clientCtx.GetFromAddress().String(),
				ExchangeId:   exchangeID,
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdDepositReserve() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit-reserve [exchange-id] [amount]",
		Short: "Deposit reserve coins",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, amount, err := parseExchangeIDAndCoins(args[0], args[1])
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgDepositReserve{
				Sender:     clientCtx.GetFromAddress().String(),
				ExchangeId: exchangeID,
				Amount:     amount,
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdWithdrawReserve() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw-reserve [exchange-id] [amount] [recipient]",
		Short: "Withdraw reserve coins",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, amount, err := parseExchangeIDAndCoins(args[0], args[1])
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgWithdrawReserve{
				AdminAddress: clientCtx.GetFromAddress().String(),
				ExchangeId:   exchangeID,
				Amount:       amount,
				Recipient:    args[2],
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdWithdrawFees() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw-fees [exchange-id] [recipient] [amount]",
		Short: "Withdraw available fees",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			exchangeID, amount, err := parseExchangeIDAndCoins(args[0], args[2])
			if err != nil {
				return err
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &bexv1.MsgWithdrawFees{
				AdminAddress: clientCtx.GetFromAddress().String(),
				ExchangeId:   exchangeID,
				Recipient:    args[1],
				Amount:       amount,
			})
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parseExchangeIDAndCoins(idRaw, coinsRaw string) (uint64, []*basev1beta1.Coin, error) {
	exchangeID, err := strconv.ParseUint(idRaw, 10, 64)
	if err != nil {
		return 0, nil, err
	}
	coins, err := sdk.ParseCoinsNormalized(coinsRaw)
	if err != nil {
		return 0, nil, err
	}
	out := make([]*basev1beta1.Coin, 0, len(coins))
	for _, coin := range coins {
		out = append(out, &basev1beta1.Coin{Denom: coin.Denom, Amount: coin.Amount.String()})
	}
	return exchangeID, out, nil
}

func decodeStrictJSON(raw string, target proto.Message) error {
	return (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal([]byte(raw), target)
}
