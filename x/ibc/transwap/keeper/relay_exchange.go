package keeper

import (
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"strconv"

	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	ibcerrors "github.com/cosmos/ibc-go/v11/modules/core/errors"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/internal/telemetry"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
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

	token := types.CloneToken(data.Token)

	transferAmount, ok := sdkmath.NewIntFromString(token.Amount)
	if !ok {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "unable to parse transfer amount: %s", token.Amount)
	}

	if types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel) {
		token.Denom.Trace = token.Denom.Trace[1:]

		coin := sdk.NewCoin(types.DenomIBCDenom(token.Denom), transferAmount)

		escrowAddress := types.GetEscrowAddress(destPort, destChannel)
		if err := k.UnescrowCoin(ctx, escrowAddress, receiver, coin); err != nil {
			return err
		}
	} else {
		trace := []*transwapv1.Hop{types.NewHop(destPort, destChannel)}
		token.Denom.Trace = append(trace, token.Denom.Trace...)

		if !k.HasDenom(ctx, types.DenomHash(token.Denom)) {
			k.SetDenom(ctx, token.Denom)
		}

		voucherDenom := types.DenomIBCDenom(token.Denom)
		if !k.BankKeeper.HasDenomMetaData(ctx, voucherDenom) {
			k.SetDenomMetadata(ctx, token.Denom)
		}

		voucher := sdk.NewCoin(voucherDenom, transferAmount)

		if err := k.BankKeeper.MintCoins(
			ctx, types.ModuleName, sdk.NewCoins(voucher),
		); err != nil {
			return errorsmod.Wrap(err, "failed to mint IBC tokens")
		}

		moduleAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
		if err := k.BankKeeper.SendCoins(
			ctx, moduleAddr, receiver, sdk.NewCoins(voucher),
		); err != nil {
			return errorsmod.Wrapf(err, "failed to send coins to receiver %s", receiver.String())
		}
	}
	return nil
}

// OnRecvExchangePacket receives the input packet into the deterministic BEX
// reserve and orchestrates an outbound v1 ICS-20 packet using the BEX quote as
// the single source of truth for swap policy.
func (k Keeper) OnRecvExchangePacket(
	ctx sdk.Context,
	data types.InternalTransferRepresentation,
	sourcePort string,
	sourceChannel string,
	destPort string,
	destChannel string,
	sourceTimeoutTimestamp uint64,
) error {
	cacheCtx, writeCache := ctx.CacheContext()
	if err := k.onRecvExchangePacket(cacheCtx, data, sourcePort, sourceChannel, destPort, destChannel, sourceTimeoutTimestamp); err != nil {
		return err
	}
	writeCache()
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	return nil
}

func (k Keeper) onRecvExchangePacket(
	ctx sdk.Context,
	data types.InternalTransferRepresentation,
	sourcePort string,
	sourceChannel string,
	destPort string,
	destChannel string,
	sourceTimeoutTimestamp uint64,
) error {
	if err := data.ValidateBasic(); err != nil {
		return errorsmod.Wrapf(err, "error validating ICS-20 transfer packet data")
	}
	if err := validateInheritedTimeout(ctx, sourceTimeoutTimestamp); err != nil {
		return errorsmod.Wrap(err, "rejecting exchange packet due to insufficient inherited timeout")
	}

	exchangeID, err := parseExchangeID(data.ExchangeID)
	if err != nil {
		return err
	}
	inputDenom := types.DenomPath(data.Token.Denom)
	direction, err := k.BexKeeper.ResolveSwapDirection(ctx, exchangeID, inputDenom)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to resolve swap direction for exchange %d denom %s", exchangeID, inputDenom)
	}

	localInputCoin, err := localReceivedCoin(data, sourcePort, sourceChannel, destPort, destChannel)
	if err != nil {
		return err
	}

	reserveAddr := k.BexKeeper.GetReserveAddress(ctx, exchangeID)
	reserveAddress := reserveAddr.String()
	destReceiver := data.Receiver
	data.Receiver = reserveAddress

	allowedCtx := sdk.UnwrapSDKContext(k.BexKeeper.WithReserveReceiveAllowance(ctx, exchangeID))
	if err := k.receiveTokens(allowedCtx, data, sourcePort, sourceChannel, destPort, destChannel); err != nil {
		return err
	}

	quote, err := k.BexKeeper.QuoteSwap(ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: exchangeID,
		InputDenom: inputDenom,
		AmountIn:   data.Token.Amount,
	})
	if err != nil {
		return errorsmod.Wrapf(err, "failed to quote exchange %d", exchangeID)
	}
	if quote.GetDirection() != direction {
		return errorsmod.Wrapf(bextypes.ErrInvariantViolation, "resolved direction %s differs from quote direction %s", direction.String(), quote.GetDirection().String())
	}

	amountOut, ok := sdkmath.NewIntFromString(quote.GetAmountOut())
	if !ok || !amountOut.IsPositive() {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "invalid quote amount_out: %s", quote.GetAmountOut())
	}
	feeAmount, ok := sdkmath.NewIntFromString(quote.GetFeeAmount())
	if !ok || feeAmount.IsNegative() {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "invalid quote fee_amount: %s", quote.GetFeeAmount())
	}

	feeCoin := sdk.NewCoin(localInputCoin.Denom, feeAmount)
	outputCoin := sdk.NewCoin(quote.GetOutputDenom(), amountOut)
	outputToken, err := k.TokenFromCoin(ctx, outputCoin)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to resolve quote output denom: %s", quote.GetOutputDenom())
	}
	swapChannel, err := outboundChannelFromToken(outputToken)
	if err != nil {
		return err
	}

	packetData := types.NewFungibleTokenPacketData(types.DenomPath(outputToken.Denom), outputToken.Amount, reserveAddress, destReceiver, "Station exchange")
	sequence, err := k.transferV1Packet(ctx, swapChannel, outputToken, sourceTimeoutTimestamp, packetData)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to send swap tokens: %s", outputCoin.Denom)
	}

	if feeCoin.IsPositive() {
		if err := k.BexKeeper.CollectFee(ctx, exchangeID, feeCoin); err != nil {
			return errorsmod.Wrapf(err, "unable to collect fee: %s", feeCoin.String())
		}
		if err := k.BexKeeper.LockExchangeFee(ctx, exchangeID, feeCoin); err != nil {
			return errorsmod.Wrapf(err, "unable to lock exchange fee: %s", feeCoin.String())
		}
	}

	refundToken, err := k.TokenFromCoin(ctx, localInputCoin)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to resolve refund token: %s", localInputCoin.Denom)
	}
	refundMsg := types.NewTransferPacketData(
		destPort,
		destChannel,
		refundToken,
		reserveAddress,
		data.Sender,
		"refund coins through Guru station due to failure on the target chain",
		sourceTimeoutTimestamp,
		feeCoin,
		strconv.FormatUint(exchangeID, 10),
	)

	refundKey := GetRefundPacketDataKey(types.PortID, swapChannel, sequence)
	if err := k.SetRefundPacketData(ctx, refundKey, &refundMsg); err != nil {
		return errorsmod.Wrapf(err, "unable to set refund packet data: %s", refundKey)
	}

	if err := k.BexKeeper.RecordVolumeWindow(ctx, exchangeID, quote.GetDirection(), amountOut); err != nil {
		return errorsmod.Wrapf(err, "unable to record exchange volume for exchange %d", exchangeID)
	}

	channel, found := k.channelKeeper.GetChannel(ctx, types.PortID, swapChannel)
	if !found {
		return errorsmod.Wrapf(channeltypes.ErrChannelNotFound, "channel not found for %s/%s", types.PortID, swapChannel)
	}
	telemetry.ReportTransfer(types.PortID, swapChannel, channel.Counterparty.PortId, channel.Counterparty.ChannelId, outputToken)

	return nil
}

func (k Keeper) OnAcknowledgementExchangePacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
	ack channeltypes.Acknowledgement,
) error {
	switch ack.Response.(type) {
	case *channeltypes.Acknowledgement_Result:
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

func (k Keeper) OnTimeoutExchangePacket(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
) error {
	return k.refundPacketTokens(ctx, sourcePort, sourceChannel, data)
}

func (k Keeper) releaseExchangeRefundFee(ctx sdk.Context, refundKey string) error {
	if !k.HasRefundPacketData(ctx, refundKey) {
		return nil
	}
	refundPacket, err := k.GetRefundPacketData(ctx, refundKey)
	if err != nil {
		return err
	}
	feeCoin, err := types.ProtoCoinToSDK(refundPacket.GetFee())
	if err != nil {
		return err
	}
	if !feeCoin.IsPositive() {
		return nil
	}
	exchangeID, err := parseExchangeID(refundPacket.ExchangeId)
	if err != nil {
		return err
	}
	if err := k.BexKeeper.ReleaseExchangeFee(ctx, exchangeID, feeCoin); err != nil {
		return errorsmod.Wrapf(err, "unable to release exchange fee: %s", feeCoin.String())
	}
	return nil
}

func (k Keeper) performExchangeRefund(ctx sdk.Context, refundKey string) error {
	if !k.HasRefundPacketData(ctx, refundKey) {
		return nil
	}
	refundPacket, err := k.GetRefundPacketData(ctx, refundKey)
	if err != nil {
		return err
	}

	exchangeID, err := parseExchangeID(refundPacket.ExchangeId)
	if err != nil {
		return err
	}

	if _, err := sdk.AccAddressFromBech32(refundPacket.Sender); err != nil {
		return errorsmod.Wrapf(err, "invalid reserve address: %s", refundPacket.Sender)
	}

	feeCoin, err := types.ProtoCoinToSDK(refundPacket.GetFee())
	if err != nil {
		return err
	}
	if feeCoin.IsPositive() {
		if err := k.BexKeeper.RefundLockedFee(ctx, exchangeID, feeCoin); err != nil {
			return errorsmod.Wrapf(err, "unable to refund locked exchange fee: %s", feeCoin.String())
		}
	}

	if refundPacket.SourcePort != types.PortID {
		return errorsmod.Wrapf(bextypes.ErrInvalidRoute, "refund source port %s does not match %s", refundPacket.SourcePort, types.PortID)
	}
	if _, found := k.channelKeeper.GetChannel(ctx, refundPacket.SourcePort, refundPacket.SourceChannel); !found {
		return errorsmod.Wrapf(channeltypes.ErrChannelNotFound, "channel not found for %s/%s", refundPacket.SourcePort, refundPacket.SourceChannel)
	}

	refundToken := refundPacket.GetToken()
	if refundToken == nil || refundToken.Denom == nil {
		return errorsmod.Wrap(types.ErrInvalidDenomForTransfer, "refund packet token cannot be nil")
	}
	token := types.CloneToken(*refundToken)
	packetData := types.NewFungibleTokenPacketData(types.DenomPath(token.Denom), token.Amount, refundPacket.Sender, refundPacket.Receiver, refundPacket.Memo)
	sequence, err := k.transferV1Packet(ctx, refundPacket.SourceChannel, token, refundPacket.TimeoutTimestamp, packetData)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to send refund tokens: %s", types.DenomPath(token.Denom))
	}

	retryPacket := cloneRefundPacketForRetry(refundPacket)
	retryKey := GetRefundPacketDataKey(refundPacket.SourcePort, refundPacket.SourceChannel, sequence)
	if err := k.SetRefundPacketData(ctx, retryKey, &retryPacket); err != nil {
		return errorsmod.Wrapf(err, "unable to set retry refund packet data: %s", retryKey)
	}

	k.DeleteRefundPacketData(ctx, refundKey)

	return nil
}

func cloneRefundPacketForRetry(packet *transwapv1.TransferPacketData) transwapv1.TransferPacketData {
	retry := *packet
	retry.Fee = nil

	if packet.GetToken() != nil {
		token := types.CloneToken(*packet.GetToken())
		retry.Token = &token
	}

	return retry
}

func localReceivedCoin(
	data types.InternalTransferRepresentation,
	sourcePort string,
	sourceChannel string,
	destPort string,
	destChannel string,
) (sdk.Coin, error) {
	amount, ok := sdkmath.NewIntFromString(data.Token.Amount)
	if !ok {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrInvalidAmount, "unable to parse transfer amount: %s", data.Token.Amount)
	}

	token := types.CloneToken(data.Token)
	if types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel) {
		token.Denom.Trace = token.Denom.Trace[1:]
	} else {
		trace := []*transwapv1.Hop{types.NewHop(destPort, destChannel)}
		token.Denom.Trace = append(trace, token.Denom.Trace...)
	}

	return sdk.NewCoin(types.DenomIBCDenom(token.Denom), amount), nil
}

func outboundChannelFromToken(token transwapv1.Token) (string, error) {
	if token.Denom == nil {
		return "", errorsmod.Wrap(bextypes.ErrInvalidRoute, "quote output denom cannot be nil")
	}
	if types.DenomIsNative(token.Denom) {
		return "", errorsmod.Wrapf(bextypes.ErrInvalidRoute, "quote output denom %s has no IBC trace", types.DenomPath(token.Denom))
	}
	hop := token.Denom.Trace[0]
	if hop == nil {
		return "", errorsmod.Wrap(bextypes.ErrInvalidRoute, "quote output first hop cannot be nil")
	}
	if hop.PortId != types.PortID {
		return "", errorsmod.Wrapf(bextypes.ErrInvalidRoute, "quote output port %s does not match %s", hop.PortId, types.PortID)
	}
	return hop.ChannelId, nil
}

func parseExchangeID(raw string) (uint64, error) {
	exchangeID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || exchangeID == 0 {
		return 0, errorsmod.Wrapf(types.ErrInvalidAmount, "unable to parse exchange id: %s", raw)
	}
	return exchangeID, nil
}

func validateInheritedTimeout(ctx sdk.Context, inheritedTimeoutTimestampNano uint64) error {
	minAcceptable := uint64(ctx.BlockTime().UnixNano()) //nolint:gosec // G115: block time cannot be negative

	if inheritedTimeoutTimestampNano < minAcceptable {
		return errorsmod.Wrapf(
			ibcerrors.ErrInvalidRequest,
			"inherited timeout timestamp is too close: got %d, required at least %d",
			inheritedTimeoutTimestampNano,
			minAcceptable,
		)
	}

	return nil
}
