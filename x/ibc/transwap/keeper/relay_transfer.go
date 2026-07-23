package keeper

import (
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	ibcerrors "github.com/cosmos/ibc-go/v11/modules/core/errors"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/internal/events"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
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
	token := types.CloneToken(data.Token)

	// This is the prefix that would have been prefixed to the denomination
	// on sender chain IF and only if the token originally came from the
	// receiving chain.
	//
	// NOTE: We use SourcePort and SourceChannel here, because the counterparty
	// chain would have prefixed with DestPort and DestChannel when originally
	// receiving this token.
	if types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel) {
		// sender chain is not the source, unescrow tokens
		// remove prefix added by sender chain
		token.Denom.Trace = token.Denom.Trace[1:]

		coin, err := types.TokenToCoin(token)
		if err != nil {
			return err
		}
		escrowAddress := types.GetEscrowAddress(destPort, destChannel)
		if err := k.UnescrowCoin(ctx, escrowAddress, receiver, coin); err != nil {
			return err
		}
	} else {
		// sender chain is the source, mint vouchers
		// since SendPacket did not prefix the denomination, we must add the destination port and channel to the trace
		trace := []types.Hop{types.NewHop(destPort, destChannel)}
		token.Denom.Trace = append(trace, token.Denom.Trace...)
		if !k.HasDenom(ctx, types.DenomHash(token.Denom)) {
			k.SetDenom(ctx, token.Denom)
		}
		voucher, err := types.TokenToCoin(token)
		if err != nil {
			return err
		}
		voucherDenom := voucher.Denom
		if !k.BankKeeper.HasDenomMetaData(ctx, voucherDenom) {
			k.SetDenomMetadata(ctx, token.Denom)
		}
		events.EmitDenomEvent(ctx, token)

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
// Only tracked output/refund packets may mutate refund accounting. Missing or
// stale indexes are deterministic no-ops, which makes duplicate callbacks
// idempotent and prevents legacy fallback logic from paying twice.
func (k Keeper) OnAcknowledgementTransferPacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	sequence uint64,
	data types.InternalTransferRepresentation,
	ack channeltypes.Acknowledgement,
) error {
	switch ack.Response.(type) {
	case *channeltypes.Acknowledgement_Result, *channeltypes.Acknowledgement_Error:
	default:
		return errorsmod.Wrapf(
			ibcerrors.ErrInvalidType,
			"expected one of [%T, %T], got %T",
			channeltypes.Acknowledgement_Result{},
			channeltypes.Acknowledgement_Error{},
			ack.Response,
		)
	}
	_, err := k.handleTrackedRefundAcknowledgement(
		ctx,
		sourcePort,
		sourceChannel,
		sequence,
		data,
		ack,
	)
	return err
}

// OnTimeoutTransferPacket processes a tracked transfer packet timeout by
// restoring the currently tracked packet to the reserve and applying the
// bounded retry policy. Missing/stale metadata is an idempotent no-op.
func (k Keeper) OnTimeoutTransferPacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	sequence uint64,
	data types.InternalTransferRepresentation,
) error {
	_, err := k.handleTrackedRefundTimeout(ctx, sourcePort, sourceChannel, sequence, data)
	return err
}
