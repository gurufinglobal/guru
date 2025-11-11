package keeper

import (
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcerrors "github.com/cosmos/ibc-go/v10/modules/core/errors"
	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/internal/events"
	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

// OnRecvTransferPacket processes a cross chain fungible token transfer.
//
// If the sender chain is the source of minted tokens then vouchers will be minted
// and sent to the receiving address. Otherwise if the sender chain is sending
// back tokens this chain originally transferred to it, the tokens are
// unescrowed and sent to the receiving address.
func (k Keeper) OnRecvTransferPacket(
	ctx sdk.Context,
	data types.InternalTransferRepresentation,
	sourcePort string,
	sourceChannel string,
	destPort string,
	destChannel string,
) error {
	// validate packet data upon receiving
	if err := data.ValidateBasic(); err != nil {
		return errorsmod.Wrapf(err, "error validating ICS-20 transfer packet data")
	}
	receiver, err := sdk.AccAddressFromBech32(data.Receiver)
	if err != nil {
		return errorsmod.Wrapf(ibcerrors.ErrInvalidAddress, "failed to decode receiver address: %s", data.Receiver)
	}
	if k.IsBlockedAddr(receiver) {
		return errorsmod.Wrapf(ibcerrors.ErrUnauthorized, "%s is not allowed to receive funds", receiver)
	}
	token := data.Token

	// parse the transfer amount
	transferAmount, ok := sdkmath.NewIntFromString(token.Amount)
	if !ok {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "unable to parse transfer amount: %s", token.Amount)
	}
	// This is the prefix that would have been prefixed to the denomination
	// on sender chain IF and only if the token originally came from the
	// receiving chain.
	//
	// NOTE: We use SourcePort and SourceChannel here, because the counterparty
	// chain would have prefixed with DestPort and DestChannel when originally
	// receiving this token.
	if token.Denom.HasPrefix(sourcePort, sourceChannel) {
		// sender chain is not the source, unescrow tokens
		// remove prefix added by sender chain
		token.Denom.Trace = token.Denom.Trace[1:]

		coin := sdk.NewCoin(token.Denom.IBCDenom(), transferAmount)
		escrowAddress := types.GetEscrowAddress(destPort, destChannel)
		if err := k.UnescrowCoin(ctx, escrowAddress, receiver, coin); err != nil {
			return err
		}
	} else {
		// sender chain is the source, mint vouchers
		// since SendPacket did not prefix the denomination, we must add the destination port and channel to the trace
		trace := []types.Hop{types.NewHop(destPort, destChannel)}
		token.Denom.Trace = append(trace, token.Denom.Trace...)
		if !k.HasDenom(ctx, token.Denom.Hash()) {
			k.SetDenom(ctx, token.Denom)
		}
		voucherDenom := token.Denom.IBCDenom()
		if !k.BankKeeper.HasDenomMetaData(ctx, voucherDenom) {
			k.SetDenomMetadata(ctx, token.Denom)
		}
		events.EmitDenomEvent(ctx, token)
		voucher := sdk.NewCoin(voucherDenom, transferAmount)

		// mint new tokens if the source of the transfer is the same chain
		if err := k.BankKeeper.MintCoins(
			ctx, types.ModuleName, sdk.NewCoins(voucher),
		); err != nil {
			return errorsmod.Wrap(err, "failed to mint IBC tokens")
		}
		// send to receiver
		moduleAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
		if err := k.BankKeeper.SendCoins(
			ctx, moduleAddr, receiver, sdk.NewCoins(voucher),
		); err != nil {
			return errorsmod.Wrapf(err, "failed to send coins to receiver %s", receiver.String())
		}

	}
	// The ibc_module.go module will return the proper ack.
	return nil
}

// OnAcknowledgementTransferPacket responds to the success or failure of a packet acknowledgment
// written on the receiving chain.
//
// If the acknowledgement was a success then nothing occurs. Otherwise,
// if the acknowledgement failed, then the sender is refunded their tokens.
func (k Keeper) OnAcknowledgementTransferPacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
	ack channeltypes.Acknowledgement,
) error {
	switch ack.Response.(type) {
	case *channeltypes.Acknowledgement_Result:
		// delete refund info from store
		k.DeleteRefundPacketData(ctx, data.Receiver)
		// the acknowledgement succeeded on the receiving chain so nothing
		// needs to be executed and no error needs to be returned
		return nil
	case *channeltypes.Acknowledgement_Error:
		if err := k.refundPacketTokens(ctx, sourcePort, sourceChannel, data); err != nil {
			return err
		}

		// refund to original source chain
		if err := k.performExchangeRefund(ctx, data); err != nil {
			return err
		}

		return nil
	default:
		// if the acknowledgement is not a success or error, then return an error
		if err := k.performExchangeRefund(ctx, data); err != nil {
			return err
		}
		return errorsmod.Wrapf(ibcerrors.ErrInvalidType, "expected one of [%T, %T], got %T", channeltypes.Acknowledgement_Result{}, channeltypes.Acknowledgement_Error{}, ack.Response)
	}
}

// OnTimeoutTransferPacket processes a transfer packet timeout by refunding the tokens to the sender
func (k Keeper) OnTimeoutTransferPacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
) error {
	// refund the tokens to the sender
	if err := k.refundPacketTokens(ctx, sourcePort, sourceChannel, data); err != nil {
		return err
	}

	// refund to original source chain
	if err := k.performExchangeRefund(ctx, data); err != nil {
		return err
	}

	return nil
}
