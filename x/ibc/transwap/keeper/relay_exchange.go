package keeper

import (
	"strings"
	"time"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcerrors "github.com/cosmos/ibc-go/v10/modules/core/errors"
	bextypes "github.com/gurufinglobal/guru/v2/x/bex/types"
	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/internal/events"
	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/internal/telemetry"
	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

func (k Keeper) receiveTokens(
	ctx sdk.Context,
	data types.InternalTransferRepresentation,
	sourcePort string,
	sourceChannel string,
	destPort string,
	destChannel string,
) error {
	receiver, err := sdk.AccAddressFromBech32(data.Receiver)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to parse receiver: %s", data.Receiver)
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
	return nil
}

// OnRecvExchangePacket processes a cross chain fungible token exchange.
//
// If the sender chain is the source of minted tokens then vouchers will be minted
// and sent to the receiving address. Otherwise if the sender chain is sending
// back tokens this chain originally transferred to it, the tokens are
// unescrowed and sent to the receiving address.
func (k Keeper) OnRecvExchangePacket(
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

	// check the exchange and it supports the given denom
	exchangeId, ok := sdkmath.NewIntFromString(data.ExchangeId)
	if !ok {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "unable to parse exchange id: %s", data.ExchangeId)
	}

	exchange, err := k.BexKeeper.GetExchange(ctx, exchangeId)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to get exchange: %s", data.ExchangeId)
	}
	if exchange == nil {
		return errorsmod.Wrapf(sdkerrors.ErrNotFound, "exchange not found: %s", exchangeId.String())
	}

	sourceDenom := data.Token.Denom.Path()
	if !exchange.IsSupportedToken(sourceDenom) {
		return errorsmod.Wrapf(bextypes.ErrInvalidDenom, "exchange does not support the given denom: %s", sourceDenom)
	}

	// backup the receiver address
	destReceiver := data.Receiver

	// step 1: receive the coins into liquidity pool
	data.Receiver = exchange.ReserveAddress

	// receive tokens
	if err := k.receiveTokens(ctx, data, sourcePort, sourceChannel, destPort, destChannel); err != nil {
		return err
	}

	// step 2: prepare the swap data

	oracleData, err := k.OracleKeeper.GetOracleData(ctx, exchange.OracleRequestId)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to get oracle data: %d", exchange.OracleRequestId)
	}

	rawData := truncatePrecision(oracleData.DataSet.RawData, 18)
	rate, err := sdkmath.LegacyNewDecFromStr(rawData)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to parse rate: %s", rawData)
	}

	swapChannel, swapPort, swapDenom, rate, err := exchange.GetSwapDataWithRate(sourceDenom, rate)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to get swap data: %s", sourceDenom)
	}
	oppChannel, oppPort, oppDenom, _, err := exchange.GetOppositeSwapDataWithRate(sourceDenom, rate)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to get swap data: %s", sourceDenom)
	}

	recvAmountDec, err := sdkmath.LegacyNewDecFromStr(data.Token.Amount)
	if err != nil {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "unable to parse receive amount: %s", data.Token.Amount)
	}

	feeDec := exchange.Fee.Mul(recvAmountDec)
	feeInt := feeDec.TruncateInt()
	feeCoin := sdk.NewCoin(oppDenom, feeInt)

	swapAmountDec := recvAmountDec.Sub(feeDec).Mul(rate)
	swapAmountInt := swapAmountDec.TruncateInt()

	coin, err := sdk.ParseCoinNormalized(swapAmountInt.String() + swapDenom)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to parse swap amount: %s", swapAmountInt.String())
	}

	if !strings.HasPrefix(coin.Denom, "ibc/") {
		denom := types.ExtractDenomFromPath(coin.Denom)
		coin.Denom = denom.IBCDenom()
	}

	token, err := k.TokenFromCoin(ctx, coin)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to parse swap token: %s", coin.Denom)
	}

	// transfer fees to module address
	accAddressReserve, err := sdk.AccAddressFromBech32(exchange.ReserveAddress)
	if err != nil {
		return errorsmod.Wrapf(err, "invalid reserve address: %s", exchange.ReserveAddress)
	}
	err = k.BankKeeper.SendCoinsFromAccountToModule(ctx, accAddressReserve, bextypes.ModuleName, sdk.NewCoins(feeCoin))
	if err != nil {
		return errorsmod.Wrapf(err, "unable to send fees to module address: %s", coin.Denom)
	}
	err = k.BexKeeper.AddExchangeFees(ctx, exchangeId.String(), sdk.NewCoins(feeCoin))
	if err != nil {
		return errorsmod.Wrapf(err, "unable to add fees to collected fees: %s", feeCoin.Denom)
	}

	packetData := types.NewFungibleTokenPacketData(token.Denom.Path(), token.Amount, exchange.ReserveAddress, destReceiver, "Station exchange")

	// step 3: send the tokens to the destination
	// if a channel exists with source channel, then use IBC V1 protocol
	// otherwise use IBC V2 protocol
	channel, isIBCV1 := k.channelKeeper.GetChannel(ctx, swapPort, swapChannel)

	if isIBCV1 {
		// if a V1 channel exists for the source channel, then use IBC V1 protocol
		_, err = k.transferV1Packet(ctx, swapChannel, token, uint64(time.Now().Add(10*time.Minute).UnixNano()), packetData)
		// telemetry for transfer occurs here, in IBC V2 this is done in the onSendPacket callback
		telemetry.ReportTransfer(swapPort, swapChannel, channel.Counterparty.PortId, channel.Counterparty.ChannelId, token)
	} else {
		// otherwise try to send an IBC V2 packet, if the sourceChannel is not a IBC V2 client
		// then core IBC will return a CounterpartyNotFound error
		_, err = k.transferV2Packet(ctx, "", swapChannel, uint64(time.Now().Add(10*time.Minute).UnixNano()), packetData)
	}
	if err != nil {
		return errorsmod.Wrapf(err, "unable to send swap tokens: %s", coin.Denom)
	}

	// step 4: set refund info in store
	oppCoin, err := sdk.ParseCoinNormalized(data.Token.Amount + oppDenom)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to parse opposite swap amount: %s", data.Token.Amount)
	}

	if !strings.HasPrefix(oppCoin.Denom, "ibc/") {
		denom := types.ExtractDenomFromPath(oppCoin.Denom)
		oppCoin.Denom = denom.IBCDenom()
	}

	oppToken, err := k.TokenFromCoin(ctx, oppCoin)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to parse opposite swap token: %s", oppCoin.Denom)
	}

	refundMsg := types.NewTransferPacketData(
		oppPort,
		oppChannel,
		oppToken,
		exchange.ReserveAddress,
		data.Sender,
		"refund coins through Guru station due to failure on the target chain",
		uint64(time.Now().Add(20*time.Minute).UnixNano()),
		feeCoin,
		exchangeId.String(),
	)

	k.SetRefundPacketData(ctx, destReceiver, &refundMsg)

	// The ibc_module.go module will return the proper ack.
	return nil
}

// OnAcknowledgementExchangePacket responds to the success or failure of a packet acknowledgment
// written on the receiving chain.
//
// If the acknowledgement was a success then nothing occurs. Otherwise,
// if the acknowledgement failed, then the sender is refunded their tokens.
func (k Keeper) OnAcknowledgementExchangePacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
	ack channeltypes.Acknowledgement,
) error {
	switch ack.Response.(type) {
	case *channeltypes.Acknowledgement_Result:
		// the acknowledgement succeeded on the receiving chain so nothing
		// needs to be executed and no error needs to be returned
		return nil
	case *channeltypes.Acknowledgement_Error:
		if err := k.refundPacketTokens(ctx, sourcePort, sourceChannel, data); err != nil {
			return err
		}
		return nil
	default:
		return errorsmod.Wrapf(ibcerrors.ErrInvalidType, "expected one of [%T, %T], got %T", channeltypes.Acknowledgement_Result{}, channeltypes.Acknowledgement_Error{}, ack.Response)
	}
}

// OnTimeoutExchangePacket processes a exchange packet timeout by refunding the tokens to the sender
func (k Keeper) OnTimeoutExchangePacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
) error {
	return k.refundPacketTokens(ctx, sourcePort, sourceChannel, data)
}

func (k Keeper) performExchangeRefund(ctx sdk.Context, data types.InternalTransferRepresentation) error {

	// refund to original source chain
	refundPacket, err := k.GetRefundPacketData(ctx, data.Receiver)
	if err != nil {
		return err
	}

	senderAcc, err := sdk.AccAddressFromBech32(refundPacket.Sender)
	if err != nil {
		return errorsmod.Wrapf(err, "invalid sender address: %s", refundPacket.Sender)
	}

	// return the fees from module to reserve address
	err = k.BankKeeper.SendCoinsFromModuleToAccount(ctx, bextypes.ModuleName, senderAcc, sdk.NewCoins(refundPacket.Fee))
	if err != nil {
		return errorsmod.Wrapf(err, "unable to send fees to sender: %s", refundPacket.Fee.Denom)
	}

	// update the kv store
	err = k.BexKeeper.DeductExchangeFees(ctx, refundPacket.ExchangeId, sdk.NewCoins(refundPacket.Fee))
	if err != nil {
		return errorsmod.Wrapf(err, "unable to deduct fees from collected fees: %s", refundPacket.Fee.Denom)
	}

	// send back to original chain
	_, isIBCV1 := k.channelKeeper.GetChannel(ctx, refundPacket.SourcePort, refundPacket.SourceChannel)
	packetData := types.NewFungibleTokenPacketData(refundPacket.Token.Denom.Path(), refundPacket.Token.Amount, refundPacket.Sender, refundPacket.Receiver, refundPacket.Memo)

	if isIBCV1 {
		// if a V1 channel exists for the source channel, then use IBC V1 protocol
		_, err = k.transferV1Packet(ctx, refundPacket.SourceChannel, refundPacket.Token, uint64(time.Now().Add(10*time.Minute).UnixNano()), packetData)
	} else {
		// otherwise try to send an IBC V2 packet, if the sourceChannel is not a IBC V2 client
		// then core IBC will return a CounterpartyNotFound error
		_, err = k.transferV2Packet(ctx, "", refundPacket.SourceChannel, uint64(time.Now().Add(10*time.Minute).UnixNano()), packetData)
	}
	if err != nil {
		return errorsmod.Wrapf(err, "unable to send refund tokens: %s", refundPacket.Token.Denom.Path())
	}

	// delete refund info from store
	k.DeleteRefundPacketData(ctx, data.Receiver)

	return nil
}

// truncatePrecision truncates the decimal precision to maxPrecision digits
func truncatePrecision(value string, maxPrecision int) string {
	parts := strings.Split(value, ".")
	if len(parts) == 2 && len(parts[1]) > maxPrecision {
		return parts[0] + "." + parts[1][:maxPrecision]
	}
	return value
}
