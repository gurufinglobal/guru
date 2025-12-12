package keeper

import (
	"fmt"
	"strings"

	ibcerrors "github.com/cosmos/ibc-go/v10/modules/core/errors"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

// SendTransfer handles transfer sending logic. There are 2 possible cases:
//
// 1. Sender chain is acting as the source zone. The coins are transferred
// to an escrow address (i.e locked) on the sender chain and then transferred
// to the receiving chain through IBC TAO logic. It is expected that the
// receiving chain will mint vouchers to the receiving address.
//
// 2. Sender chain is acting as the sink zone. The coins (vouchers) are burned
// on the sender chain and then transferred to the receiving chain though IBC
// TAO logic. It is expected that the receiving chain, which had previously
// sent the original denomination, will unescrow the fungible token and send
// it to the receiving address.
//
// Another way of thinking of source and sink zones is through the token's
// timeline. Each send to any chain other than the one it was previously
// received from is a movement forwards in the token's timeline. This causes
// trace to be added to the token's history and the destination port and
// destination channel to be prefixed to the denomination. In these instances
// the sender chain is acting as the source zone. When the token is sent back
// to the chain it previously received from, the prefix is removed. This is
// a backwards movement in the token's timeline and the sender chain
// is acting as the sink zone.
//
// Example:
// These steps of transfer occur: A -> B -> C -> A -> C -> B -> A
//
// 1. A -> B : sender chain is source zone. Denom upon receiving: 'B/denom'
// 2. B -> C : sender chain is source zone. Denom upon receiving: 'C/B/denom'
// 3. C -> A : sender chain is source zone. Denom upon receiving: 'A/C/B/denom'
// 4. A -> C : sender chain is sink zone. Denom upon receiving: 'C/B/denom'
// 5. C -> B : sender chain is sink zone. Denom upon receiving: 'B/denom'
// 6. B -> A : sender chain is sink zone. Denom upon receiving: 'denom'
func (k Keeper) SendTransfer(
	ctx sdk.Context,
	sourcePort,
	sourceChannel string,
	token types.Token,
	sender sdk.AccAddress,
) error {
	if k.IsBlockedAddr(sender) {
		return errorsmod.Wrapf(ibcerrors.ErrUnauthorized, "%s is not allowed to send funds", sender)
	}

	coin, err := token.ToCoin()
	if err != nil {
		return err
	}

	if err := k.BankKeeper.IsSendEnabledCoins(ctx, coin); err != nil {
		return errorsmod.Wrap(types.ErrSendDisabled, err.Error())
	}

	// NOTE: SendTransfer simply sends the denomination as it exists on its own
	// chain inside the packet data. The receiving chain will perform denom
	// prefixing as necessary.

	// if the denom is prefixed by the port and channel on which we are sending
	// the token, then we must be returning the token back to the chain they originated from
	if token.Denom.HasPrefix(sourcePort, sourceChannel) {
		// transfer the coins to the module account and burn them
		if err := k.BankKeeper.SendCoinsFromAccountToModule(
			ctx, sender, types.ModuleName, sdk.NewCoins(coin),
		); err != nil {
			return err
		}

		if err := k.BankKeeper.BurnCoins(
			ctx, types.ModuleName, sdk.NewCoins(coin),
		); err != nil {
			// NOTE: should not happen as the module account was
			// retrieved on the step above and it has enough balance
			// to burn.
			panic(fmt.Errorf("cannot burn coins after a successful send to a module account: %v", err))
		}
	} else {
		// obtain the escrow address for the source channel end
		escrowAddress := types.GetEscrowAddress(sourcePort, sourceChannel)
		if err := k.EscrowCoin(ctx, sender, escrowAddress, coin); err != nil {
			return err
		}
	}

	return nil
}

// refundPacketTokens will unescrow and send back the token back to sender
// if the sending chain was the source chain. Otherwise, the sent token
// were burnt in the original send so new tokens are minted and sent to
// the sending address.
func (k Keeper) refundPacketTokens(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
) error {
	// NOTE: packet data type already checked in handler.go

	sender, err := sdk.AccAddressFromBech32(data.Sender)
	if err != nil {
		return err
	}
	if k.IsBlockedAddr(sender) {
		return errorsmod.Wrapf(ibcerrors.ErrUnauthorized, "%s is not allowed to receive funds", sender)
	}

	// escrow address for unescrowing tokens back to sender
	escrowAddress := types.GetEscrowAddress(sourcePort, sourceChannel)

	moduleAccountAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
	token := data.Token
	coin, err := token.ToCoin()
	if err != nil {
		return err
	}

	// if the token we must refund is prefixed by the source port and channel
	// then the tokens were burnt when the packet was sent and we must mint new tokens
	if token.Denom.HasPrefix(sourcePort, sourceChannel) {
		// mint vouchers back to sender
		if err := k.BankKeeper.MintCoins(
			ctx, types.ModuleName, sdk.NewCoins(coin),
		); err != nil {
			return err
		}

		if err := k.BankKeeper.SendCoins(ctx, moduleAccountAddr, sender, sdk.NewCoins(coin)); err != nil {
			panic(fmt.Errorf("unable to send coins from module to account despite previously minting coins to module account: %v", err))
		}
	} else {
		if err := k.UnescrowCoin(ctx, escrowAddress, sender, coin); err != nil {
			return err
		}
	}

	return nil
}

// EscrowCoin will send the given coin from the provided sender to the escrow address. It will also
// update the total escrowed amount by adding the escrowed coin's amount to the current total escrow.
func (k Keeper) EscrowCoin(ctx sdk.Context, sender, escrowAddress sdk.AccAddress, coin sdk.Coin) error {
	if err := k.BankKeeper.SendCoins(ctx, sender, escrowAddress, sdk.NewCoins(coin)); err != nil {
		// failure is expected for insufficient balances
		return err
	}

	// track the total amount in escrow keyed by denomination to allow for efficient iteration
	currentTotalEscrow := k.GetTotalEscrowForDenom(ctx, coin.GetDenom())
	newTotalEscrow := currentTotalEscrow.Add(coin)
	k.SetTotalEscrowForDenom(ctx, newTotalEscrow)

	return nil
}

// UnescrowCoin will send the given coin from the escrow address to the provided receiver. It will also
// update the total escrow by deducting the unescrowed coin's amount from the current total escrow.
func (k Keeper) UnescrowCoin(ctx sdk.Context, escrowAddress, receiver sdk.AccAddress, coin sdk.Coin) error {
	if err := k.BankKeeper.SendCoins(ctx, escrowAddress, receiver, sdk.NewCoins(coin)); err != nil {
		// NOTE: this error is only expected to occur given an unexpected bug or a malicious
		// counterparty module. The bug may occur in bank or any part of the code that allows
		// the escrow address to be drained. A malicious counterparty module could drain the
		// escrow address by allowing more tokens to be sent back then were escrowed.
		return errorsmod.Wrap(err, "unable to unescrow tokens, this may be caused by a malicious counterparty module or a bug: please open an issue on counterparty module")
	}

	// track the total amount in escrow keyed by denomination to allow for efficient iteration
	currentTotalEscrow := k.GetTotalEscrowForDenom(ctx, coin.GetDenom())
	newTotalEscrow := currentTotalEscrow.Sub(coin)
	k.SetTotalEscrowForDenom(ctx, newTotalEscrow)

	return nil
}

// tokenFromCoin constructs an IBC token given an SDK coin.
func (k Keeper) TokenFromCoin(ctx sdk.Context, coin sdk.Coin) (types.Token, error) {
	// if the coin does not have an IBC denom, return as is
	if !strings.HasPrefix(coin.Denom, "ibc/") {
		return types.Token{
			Denom:  types.NewDenom(coin.Denom),
			Amount: coin.Amount.String(),
		}, nil
	}

	// NOTE: denomination and hex hash correctness checked during msg.ValidateBasic
	denom, err := k.GetDenomFromIBCDenom(ctx, coin.Denom)
	if err != nil {
		return types.Token{}, err
	}

	return types.Token{
		Denom:  denom,
		Amount: coin.Amount.String(),
	}, nil
}

// GetDenomFromIBCDenom returns the `Denom` given the IBC Denom (ibc/{hex hash}) of the denomination.
// The ibcDenom is the hex hash of the denomination prefixed by "ibc/", often referred to as the IBC denom.
func (k Keeper) GetDenomFromIBCDenom(ctx sdk.Context, ibcDenom string) (types.Denom, error) {
	hexHash := ibcDenom[len(types.DenomPrefix+"/"):]

	hash, err := types.ParseHexHash(hexHash)
	if err != nil {
		return types.Denom{}, errorsmod.Wrap(types.ErrInvalidDenomForTransfer, err.Error())
	}

	denom, found := k.GetDenom(ctx, hash)
	if !found {
		return types.Denom{}, errorsmod.Wrap(types.ErrDenomNotFound, hexHash)
	}

	return denom, nil
}

// Deprecated: usage of this function should be replaced by `Keeper.GetDenomFromIBCDenom`
// DenomPathFromHash returns the full denomination path prefix from an ibc denom with a hash
// component.
func (k Keeper) DenomPathFromHash(ctx sdk.Context, ibcDenom string) (string, error) {
	denom, err := k.GetDenomFromIBCDenom(ctx, ibcDenom)
	if err != nil {
		return "", err
	}

	return denom.Path(), nil
}

// createPacketDataBytesFromVersion creates the packet data bytes to be sent based on the application version.
// func createPacketDataBytesFromVersion(appVersion, sender, receiver, memo string, token types.Token) ([]byte, error) {
// 	switch appVersion {
// 	case types.V1:
// 		packetData := types.NewFungibleTokenPacketData(token.Denom.Path(), token.Amount, sender, receiver, memo)

// 		if err := packetData.ValidateBasic(); err != nil {
// 			return nil, errorsmod.Wrapf(err, "failed to validate %s packet data", types.V1)
// 		}

// 		return packetData.GetBytes(), nil
// 	default:
// 		return nil, errorsmod.Wrapf(types.ErrInvalidVersion, "app version must be one of %s", types.SupportedVersions)
// 	}
// }
