package cmd

import (
	"fmt"
	"strings"

	"cosmossdk.io/core/address"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/ethereum/go-ethereum/common"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/spf13/cobra"
)

func patchEVMSendCommand(rootCmd *cobra.Command) {
	cmd, _, err := rootCmd.Find([]string{"tx", "evm", "send"})
	if err != nil || cmd == nil {
		return
	}

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		fromAddr := args[0]
		if strings.HasPrefix(fromAddr, "0x") {
			fromBytes, err := parseEVMSendAddress(accountCodec, fromAddr)
			if err != nil {
				return fmt.Errorf("invalid from address: %w", err)
			}
			fromAddr, err = accountCodec.BytesToString(fromBytes)
			if err != nil {
				return err
			}
		}
		if err := cmd.Flags().Set(flags.FlagFrom, fromAddr); err != nil {
			return err
		}

		clientCtx, err := client.GetClientTxContext(cmd)
		if err != nil {
			return err
		}

		toAddr, err := parseEVMSendAddress(accountCodec, args[1])
		if err != nil {
			return fmt.Errorf("invalid recipient address: %w", err)
		}

		coins, err := sdk.ParseCoinsNormalized(args[2])
		if err != nil {
			return err
		}
		if len(coins) == 0 {
			return fmt.Errorf("invalid coins")
		}

		msg := banktypes.NewMsgSend(clientCtx.GetFromAddress(), toAddr, coins)
		return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
	}
}

func parseEVMSendAddress(accountCodec address.Codec, raw string) ([]byte, error) {
	if strings.HasPrefix(raw, "0x") {
		if !common.IsHexAddress(raw) {
			return nil, fmt.Errorf("expected 20-byte hex address")
		}

		return common.HexToAddress(raw).Bytes(), nil
	}

	return accountCodec.StringToBytes(raw)
}
