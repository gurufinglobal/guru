package cli

import (
	"fmt"
	"os"
	"strings"

	coreaddress "cosmossdk.io/core/address"
	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

var (
	getClientTxContext       = client.GetClientTxContext
	generateOrBroadcastTxCLI = tx.GenerateOrBroadcastTxCLI
)

func NewChangeModeratorTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change_moderator [new_moderator_address] --from [moderator_address]",
		Short: "Change the shared Constitution moderator through the legacy feepolicy API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCLIAddressText(args[0]); err != nil {
				return err
			}
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			accountCodec, err := signingAddressCodec(clientCtx)
			if err != nil {
				return err
			}
			if err := validateCLIModeratorAddress(accountCodec, args[0]); err != nil {
				return err
			}

			// Address validation and Hex/Bech32 normalization use the keeper's
			// injected EVM codec when the message executes.
			msg := &types.MsgChangeModerator{
				ModeratorAddress:    clientCtx.GetFromAddress().String(),
				NewModeratorAddress: args[0],
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewRegisterDiscountsTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register_discounts [path_to_json] --from [moderator_address]",
		Short: "Register discounts from inline JSON or a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}

			cdc := codec.NewProtoCodec(clientCtx.InterfaceRegistry)
			var discounts types.AccountDiscounts
			if err := cdc.UnmarshalJSON([]byte(args[0]), &discounts); err != nil {
				contents, readErr := os.ReadFile(args[0])
				if readErr != nil {
					return errorsmod.Wrapf(types.ErrInvalidJSONFile, "%v", readErr)
				}
				if err := cdc.UnmarshalJSON(contents, &discounts); err != nil {
					return errorsmod.Wrapf(types.ErrInvalidJSONFile, "%v", err)
				}
			}

			msg := &types.MsgRegisterDiscounts{
				ModeratorAddress: clientCtx.GetFromAddress().String(),
				Discounts:        discounts.Discounts,
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewRemoveDiscountsTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove_discounts [discount_address|global] [module] [msg_type] --from [moderator_address]",
		Short: "Remove discounts for an address or the global policy",
		Long: "Remove an entire policy, one module, or one message-type rule. " +
			"When msg_type is supplied, the non-empty module argument selects the legacy message-removal branch; " +
			"that branch removes the first matching message type from every module.",
		Example: "gurud tx feepolicy remove_discounts global --from moderator\n" +
			"gurud tx feepolicy remove_discounts guru1... bank /cosmos.bank.v1beta1.MsgSend --from moderator",
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			address, global := parsePolicyAddress(args[0])
			if !global {
				if err := validateCLIAddressText(address); err != nil {
					return err
				}
			}
			if len(args) >= 2 && args[1] == "" {
				return sdkerrors.ErrInvalidRequest.Wrap("module cannot be empty when provided")
			}
			if len(args) == 3 && args[2] == "" {
				return sdkerrors.ErrInvalidRequest.Wrap("msg type cannot be empty when provided")
			}
			clientCtx, err := getClientTxContext(cmd)
			if err != nil {
				return err
			}
			if !global {
				accountCodec, err := signingAddressCodec(clientCtx)
				if err != nil {
					return err
				}
				if err := validateCLIAccountAddress(accountCodec, address); err != nil {
					return err
				}
			}
			msg := &types.MsgRemoveDiscounts{
				ModeratorAddress: clientCtx.GetFromAddress().String(),
				Address:          address,
			}
			if len(args) >= 2 {
				msg.Module = args[1]
			}
			if len(args) == 3 {
				msg.MsgType = args[2]
			}
			return generateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func validateCLIAddressText(raw string) error {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return sdkerrors.ErrInvalidAddress.Wrap("address cannot be empty or contain surrounding whitespace")
	}
	return nil
}

func validateCLIAccountAddress(accountCodec coreaddress.Codec, raw string) error {
	decoded, err := decodeCLIAddress(accountCodec, raw)
	if err != nil {
		return err
	}
	if len(decoded) != common.AddressLength {
		return sdkerrors.ErrInvalidAddress.Wrap(fmt.Sprintf("address must be %d bytes, got %d", common.AddressLength, len(decoded)))
	}
	return nil
}

func validateCLIModeratorAddress(accountCodec coreaddress.Codec, raw string) error {
	_, err := decodeCLIAddress(accountCodec, raw)
	return err
}

func decodeCLIAddress(accountCodec coreaddress.Codec, raw string) ([]byte, error) {
	if err := validateCLIAddressText(raw); err != nil {
		return nil, err
	}
	if accountCodec == nil {
		return nil, sdkerrors.ErrInvalidAddress.Wrap("signing address codec is not configured")
	}
	decoded, err := accountCodec.StringToBytes(raw)
	if err != nil {
		return nil, sdkerrors.ErrInvalidAddress.Wrap(err.Error())
	}
	return decoded, nil
}

func signingAddressCodec(clientCtx client.Context) (coreaddress.Codec, error) {
	if clientCtx.InterfaceRegistry == nil || clientCtx.InterfaceRegistry.SigningContext() == nil {
		return nil, sdkerrors.ErrInvalidAddress.Wrap("signing context is not configured")
	}
	accountCodec := clientCtx.InterfaceRegistry.SigningContext().AddressCodec()
	if accountCodec == nil {
		return nil, sdkerrors.ErrInvalidAddress.Wrap("signing address codec is not configured")
	}
	return accountCodec, nil
}
