package cmd

import (
	"bufio"
	"fmt"
	"math/big"
	"strings"

	"cosmossdk.io/core/address"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/input"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/spf13/cobra"

	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

// newEVMTxCommand exposes the upstream EVM transaction surface while keeping
// the Cosmos and EVM signing domains distinct. Guru's Cosmos chain ID is
// guru_631-1, whereas signed Ethereum transactions use EIP-155 chain ID 631.
func newEVMTxCommand(accountCodec address.Codec) *cobra.Command {
	command := &cobra.Command{
		Use:                        evmtypes.ModuleName,
		Short:                      "EVM transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	command.AddCommand(
		newRawEVMTxCommand(),
		newEVMSendTxCommand(accountCodec),
	)
	return command
}

// newRawEVMTxCommand wraps a signed Ethereum transaction in the canonical
// Cosmos EVM envelope and broadcasts it through CometBFT.
func newRawEVMTxCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "raw TX_HEX",
		Short: "Broadcast a signed Ethereum transaction",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(command)
			if err != nil {
				return err
			}
			if clientCtx.ChainID != chainconfig.LocalChainID {
				return fmt.Errorf(
					"Cosmos chain ID must be %q, got %q",
					chainconfig.LocalChainID,
					clientCtx.ChainID,
				)
			}

			raw, err := hexutil.Decode(args[0])
			if err != nil {
				return fmt.Errorf("decode Ethereum transaction: %w", err)
			}
			ethereumTx := new(ethtypes.Transaction)
			if err := ethereumTx.UnmarshalBinary(raw); err != nil {
				return fmt.Errorf("decode Ethereum transaction: %w", err)
			}

			expectedEVMChainID := new(big.Int).SetUint64(chainconfig.EVMChainID)
			if !ethereumTx.Protected() {
				return fmt.Errorf("Ethereum transaction must use EIP-155 replay protection")
			}
			if ethereumTx.ChainId().Cmp(expectedEVMChainID) != 0 {
				return fmt.Errorf(
					"Ethereum chain ID must be %d, got %s",
					chainconfig.EVMChainID,
					ethereumTx.ChainId(),
				)
			}

			message := new(evmtypes.MsgEthereumTx)
			signer := ethtypes.LatestSignerForChainID(expectedEVMChainID)
			if err := message.FromSignedEthereumTx(ethereumTx, signer); err != nil {
				return fmt.Errorf("recover Ethereum transaction signer: %w", err)
			}
			if err := message.ValidateBasic(); err != nil {
				return err
			}

			paramsResponse, err := evmtypes.NewQueryClient(clientCtx).Params(
				command.Context(),
				&evmtypes.QueryParamsRequest{},
			)
			if err != nil {
				return fmt.Errorf("query EVM params: %w", err)
			}
			cosmosTx, err := message.BuildTxWithEvmParams(
				clientCtx.TxConfig.NewTxBuilder(),
				paramsResponse.Params,
			)
			if err != nil {
				return err
			}

			if clientCtx.GenerateOnly {
				encoded, err := clientCtx.TxConfig.TxJSONEncoder()(cosmosTx)
				if err != nil {
					return err
				}
				return clientCtx.PrintString(fmt.Sprintf("%s\n", encoded))
			}

			if !clientCtx.SkipConfirm {
				encoded, err := clientCtx.TxConfig.TxJSONEncoder()(cosmosTx)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(command.ErrOrStderr(), "%s\n\n", encoded); err != nil {
					return err
				}
				confirmed, err := input.GetConfirmation(
					"confirm transaction before broadcasting",
					bufio.NewReader(command.InOrStdin()),
					command.ErrOrStderr(),
				)
				if err != nil {
					return err
				}
				if !confirmed {
					return fmt.Errorf("transaction broadcast canceled")
				}
			}

			txBytes, err := clientCtx.TxConfig.TxEncoder()(cosmosTx)
			if err != nil {
				return err
			}
			response, err := clientCtx.BroadcastTx(txBytes)
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(response)
		},
	}
	flags.AddTxFlagsToCmd(command)
	return command
}

// newEVMSendTxCommand preserves the upstream convenience command: it creates
// a Cosmos bank MsgSend while accepting either Guru bech32 or Ethereum hex
// account addresses.
func newEVMSendTxCommand(accountCodec address.Codec) *cobra.Command {
	command := &cobra.Command{
		Use:   "send [from_key_or_address] [to_address] [amount]",
		Short: "Send native funds using bech32 or Ethereum hex addresses",
		Args:  cobra.ExactArgs(3),
		RunE: func(command *cobra.Command, args []string) error {
			from := args[0]
			if common.IsHexAddress(from) {
				encodedFrom, err := accountCodec.BytesToString(common.HexToAddress(from).Bytes())
				if err != nil {
					return fmt.Errorf("encode sender Ethereum address %q: %w", args[0], err)
				}
				from = encodedFrom
			} else if strings.HasPrefix(strings.ToLower(from), "0x") {
				return fmt.Errorf("invalid sender Ethereum address %q", from)
			}
			if err := command.Flags().Set(flags.FlagFrom, from); err != nil {
				return err
			}

			clientCtx, err := client.GetClientTxContext(command)
			if err != nil {
				return err
			}
			to, err := parseEVMCompatibleAccountAddress(accountCodec, args[1])
			if err != nil {
				return err
			}
			coins, err := sdk.ParseCoinsNormalized(args[2])
			if err != nil {
				return err
			}
			if coins.Empty() {
				return fmt.Errorf("amount must contain at least one coin")
			}

			message := banktypes.NewMsgSend(clientCtx.GetFromAddress(), to, coins)
			return tx.GenerateOrBroadcastTxCLI(clientCtx, command.Flags(), message)
		},
	}
	flags.AddTxFlagsToCmd(command)
	return command
}

func parseEVMCompatibleAccountAddress(accountCodec address.Codec, raw string) ([]byte, error) {
	if common.IsHexAddress(raw) {
		return common.HexToAddress(raw).Bytes(), nil
	}
	addressBytes, err := accountCodec.StringToBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address %q: %w", raw, err)
	}
	return addressBytes, nil
}
