package keeper

import (
	"math"
	"strconv"
	"time"

	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	ibcerrors "github.com/cosmos/ibc-go/v11/modules/core/errors"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/internal/telemetry"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const (
	minimumForwardTimeout = time.Minute
	maximumForwardTimeout = types.MaximumRefundTimeoutWindow
)

func (k Keeper) receiveTokensToReserve(
	ctx sdk.Context,
	exchangeID uint64,
	data types.InternalTransferRepresentation,
	sourcePort string,
	sourceChannel string,
	destPort string,
	destChannel string,
) error {
	token := types.CloneToken(data.Token)
	if token == nil {
		return errorsmod.Wrap(types.ErrInvalidDenomForTransfer, "input token cannot be nil")
	}

	if types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel) {
		token.Denom.Trace = token.Denom.Trace[1:]

		coin, err := types.TokenToCoin(token)
		if err != nil {
			return err
		}

		escrowAddress := types.GetEscrowAddress(destPort, destChannel)
		currentTotalEscrow := k.GetTotalEscrowForDenom(ctx, coin.Denom)
		if currentTotalEscrow.Amount.LT(coin.Amount) {
			return types.ErrRefundEscrowInvariant.Wrapf(
				"tracked escrow %s cannot release input %s",
				currentTotalEscrow,
				coin,
			)
		}
		if err := k.BexKeeper.ReceiveToReserve(ctx, exchangeID, escrowAddress, sdk.NewCoins(coin)); err != nil {
			return err
		}
		k.SetTotalEscrowForDenom(ctx, currentTotalEscrow.Sub(coin))
	} else {
		trace := []*transwapv1.Hop{types.NewHop(destPort, destChannel)}
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

		if err := k.BankKeeper.MintCoins(
			ctx, types.ModuleName, sdk.NewCoins(voucher),
		); err != nil {
			return errorsmod.Wrap(err, "failed to mint IBC tokens")
		}

		moduleAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
		if err := k.BexKeeper.ReceiveToReserve(ctx, exchangeID, moduleAddr, sdk.NewCoins(voucher)); err != nil {
			return errorsmod.Wrap(err, "failed to move minted IBC tokens into reserve")
		}
	}
	return nil
}

func (k Keeper) sendSwapOutputFromReserve(
	ctx sdk.Context,
	exchangeID uint64,
	sourceChannel string,
	token *transwapv1.Token,
) error {
	coin, err := types.TokenToCoin(token)
	if err != nil {
		return err
	}
	if err := k.BankKeeper.IsSendEnabledCoins(ctx, coin); err != nil {
		return errorsmod.Wrap(types.ErrSendDisabled, err.Error())
	}

	if types.DenomHasPrefix(token.Denom, types.PortID, sourceChannel) {
		moduleAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
		if err := k.BexKeeper.SendSwapOutputFromReserve(ctx, exchangeID, moduleAddr, coin); err != nil {
			return err
		}
		if err := k.BankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return errorsmod.Wrap(err, "failed to burn reserve voucher after custody transfer")
		}
		return nil
	}

	escrowAddress := types.GetEscrowAddress(types.PortID, sourceChannel)
	if err := k.BexKeeper.SendSwapOutputFromReserve(ctx, exchangeID, escrowAddress, coin); err != nil {
		return err
	}
	currentTotalEscrow := k.GetTotalEscrowForDenom(ctx, coin.Denom)
	k.SetTotalEscrowForDenom(ctx, currentTotalEscrow.Add(coin))
	return nil
}

func (k Keeper) refundPacketTokensToReserve(
	ctx sdk.Context,
	exchangeID uint64,
	sourcePort string,
	sourceChannel string,
	data types.InternalTransferRepresentation,
) error {
	coin, err := types.TokenToCoin(data.Token)
	if err != nil {
		return err
	}
	coins := sdk.NewCoins(coin)
	if types.DenomHasPrefix(data.Token.Denom, sourcePort, sourceChannel) {
		if err := k.BankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
			return err
		}
		moduleAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
		return k.BexKeeper.ReceiveToReserve(ctx, exchangeID, moduleAddr, coins)
	}

	escrowAddress := types.GetEscrowAddress(sourcePort, sourceChannel)
	currentTotalEscrow := k.GetTotalEscrowForDenom(ctx, coin.Denom)
	if currentTotalEscrow.Amount.LT(coin.Amount) {
		return types.ErrRefundEscrowInvariant.Wrapf(
			"tracked escrow %s cannot restore failed output %s",
			currentTotalEscrow,
			coin,
		)
	}
	if err := k.BexKeeper.ReceiveToReserve(ctx, exchangeID, escrowAddress, coins); err != nil {
		return err
	}
	k.SetTotalEscrowForDenom(ctx, currentTotalEscrow.Sub(coin))
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
	sourceTimeoutHeights ...clienttypes.Height,
) error {
	sourceTimeoutHeight := clienttypes.ZeroHeight()
	if len(sourceTimeoutHeights) > 0 {
		sourceTimeoutHeight = sourceTimeoutHeights[0]
	}
	cacheCtx, writeCache := ctx.CacheContext()
	if err := k.onRecvExchangePacket(cacheCtx, data, sourcePort, sourceChannel, destPort, destChannel, sourceTimeoutTimestamp, sourceTimeoutHeight); err != nil {
		return err
	}
	writeCache()
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
	sourceTimeoutHeight clienttypes.Height,
) error {
	if err := data.ValidateBasic(); err != nil {
		return errorsmod.Wrapf(err, "error validating ICS-20 transfer packet data")
	}
	protection, err := types.ParseSwapProtection(data.Memo)
	if err != nil {
		return err
	}
	if err := validateInheritedTimeout(ctx, sourceTimeoutTimestamp); err != nil {
		return errorsmod.Wrap(err, "rejecting exchange packet due to insufficient inherited timeout")
	}

	exchangeID, err := parseExchangeID(data.ExchangeID)
	if err != nil {
		return err
	}
	localInputCoin, err := localReceivedCoin(data, sourcePort, sourceChannel, destPort, destChannel)
	if err != nil {
		return err
	}
	inputDenom := types.DenomPath(data.Token.Denom)
	direction, err := k.BexKeeper.ValidateSwapInput(ctx, exchangeID, inputDenom, localInputCoin.Denom)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to validate swap input for exchange %d denom %s", exchangeID, inputDenom)
	}

	reserveAddr := k.BexKeeper.GetReserveAddress(ctx, exchangeID)
	reserveAddress := reserveAddr.String()
	destReceiver := data.Receiver
	if err := k.receiveTokensToReserve(ctx, exchangeID, data, sourcePort, sourceChannel, destPort, destChannel); err != nil {
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
	if quote == nil {
		return errorsmod.Wrapf(bextypes.ErrInvariantViolation, "exchange %d returned a nil quote", exchangeID)
	}
	if quote.GetDirection() != direction {
		return errorsmod.Wrapf(bextypes.ErrInvariantViolation, "resolved direction %s differs from quote direction %s", direction.String(), quote.GetDirection().String())
	}

	amountOut, err := uint256decimal.ParseCanonicalPositive(quote.GetAmountOut())
	if err != nil {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "invalid quote amount_out: %s", quote.GetAmountOut())
	}
	if protection.HasExpectedExchangeRevision && quote.GetExchangeRevision() != protection.ExpectedExchangeRevision {
		return errorsmod.Wrapf(
			bextypes.ErrRevisionConflict,
			"expected exchange revision %d, got %d",
			protection.ExpectedExchangeRevision,
			quote.GetExchangeRevision(),
		)
	}
	if protection.HasMinAmountOut && amountOut.LT(protection.MinAmountOut) {
		return errorsmod.Wrapf(
			types.ErrMinimumAmountOut,
			"quoted amount_out %s is below minimum %s",
			amountOut,
			protection.MinAmountOut,
		)
	}
	feeAmount, err := uint256decimal.ParseCanonical(quote.GetFeeAmount())
	if err != nil {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "invalid quote fee_amount: %s", quote.GetFeeAmount())
	}
	if !localInputCoin.Amount.GT(feeAmount) {
		return errorsmod.Wrapf(
			bextypes.ErrInvariantViolation,
			"fee %s must be less than gross input %s",
			feeAmount,
			localInputCoin.Amount,
		)
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
	sequence, err := k.transferV1PacketFromReserve(ctx, exchangeID, swapChannel, outputToken, sourceTimeoutTimestamp, packetData)
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
	if err := k.BexKeeper.AddPendingLiability(ctx, exchangeID, localInputCoin); err != nil {
		return errorsmod.Wrapf(err, "unable to reserve pending refund liability: %s", localInputCoin.String())
	}

	claimAddress, err := claimAddressFromReceiver(data.Sender)
	if err != nil {
		return err
	}
	refundID := RefundID(types.PortID, swapChannel, sequence)
	volumeReservation, err := k.BexKeeper.ReserveVolumeWindow(ctx, exchangeID, quote.GetDirection(), amountOut)
	if err != nil {
		return errorsmod.Wrapf(err, "unable to reserve exchange volume for exchange %d", exchangeID)
	}
	if volumeReservation == nil ||
		volumeReservation.GetExchangeId() != exchangeID ||
		volumeReservation.GetDirection() != quote.GetDirection() ||
		volumeReservation.GetAmount() != amountOut.String() {
		return errorsmod.Wrapf(bextypes.ErrInvariantViolation, "exchange %d returned an invalid volume reservation", exchangeID)
	}
	originalOutputCommitment := channeltypes.CommitPacket(channeltypes.NewPacket(
		types.FungibleTokenPacketDataBytes(packetData),
		sequence,
		types.PortID,
		swapChannel,
		"",
		"",
		clienttypes.ZeroHeight(),
		sourceTimeoutTimestamp,
	))
	refundRecord := &transwapv1.RefundRecord{
		Id:                       refundID,
		Status:                   transwapv1.RefundStatus_REFUND_STATUS_PENDING,
		RefundSourcePort:         destPort,
		RefundSourceChannel:      destChannel,
		Token:                    types.CloneToken(refundToken),
		Receiver:                 data.Sender,
		ClaimAddress:             claimAddress.String(),
		Memo:                     "refund coins through Guru station due to failure on the target chain",
		ExchangeId:               strconv.FormatUint(exchangeID, 10),
		OriginalFee:              types.SDKCoinToProto(feeCoin),
		OriginalTimeoutTimestamp: sourceTimeoutTimestamp,
		OriginalTimeoutHeight: &transwapv1.RefundHeight{
			RevisionNumber: sourceTimeoutHeight.RevisionNumber,
			RevisionHeight: sourceTimeoutHeight.RevisionHeight,
		},
		OriginalOutputPort:             types.PortID,
		OriginalOutputChannel:          swapChannel,
		OriginalOutputSequence:         sequence,
		OriginalOutputPacketCommitment: originalOutputCommitment,
		VolumeReservation:              volumeReservation,
	}
	if err := k.CreateRefundRecord(ctx, refundRecord); err != nil {
		return errorsmod.Wrapf(err, "unable to create refund record %s", refundID)
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

func localReceivedCoin(
	data types.InternalTransferRepresentation,
	sourcePort string,
	sourceChannel string,
	destPort string,
	destChannel string,
) (sdk.Coin, error) {
	token := types.CloneToken(data.Token)
	if token == nil {
		return sdk.Coin{}, errorsmod.Wrap(types.ErrInvalidDenomForTransfer, "token cannot be nil")
	}
	if types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel) {
		token.Denom.Trace = token.Denom.Trace[1:]
	} else {
		trace := []*transwapv1.Hop{types.NewHop(destPort, destChannel)}
		token.Denom.Trace = append(trace, token.Denom.Trace...)
	}

	return types.TokenToCoin(token)
}

func outboundChannelFromToken(token *transwapv1.Token) (string, error) {
	if token == nil || token.Denom == nil {
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
	kind, exchangeID, err := types.ClassifyExchangeID(raw)
	if err != nil {
		return 0, err
	}
	if kind != types.PacketKindExchange {
		return 0, errorsmod.Wrapf(types.ErrInvalidExchangeID, "exchange packet requires a positive exchange id: %q", raw)
	}
	return exchangeID, nil
}

func validateInheritedTimeout(ctx sdk.Context, inheritedTimeoutTimestampNano uint64) error {
	now := ctx.BlockTime().UnixNano()
	if now < 0 || now > math.MaxInt64-int64(maximumForwardTimeout) {
		return errorsmod.Wrap(ibcerrors.ErrInvalidRequest, "block time cannot produce a safe packet timeout")
	}
	minAcceptable := uint64(now + int64(minimumForwardTimeout)) //nolint:gosec // checked non-negative above
	maxAcceptable := uint64(now + int64(maximumForwardTimeout)) //nolint:gosec // checked non-negative above

	if inheritedTimeoutTimestampNano <= minAcceptable {
		return errorsmod.Wrapf(
			ibcerrors.ErrInvalidRequest,
			"inherited timeout timestamp is too close: got %d, required at least %d",
			inheritedTimeoutTimestampNano,
			minAcceptable,
		)
	}
	if inheritedTimeoutTimestampNano > maxAcceptable {
		return errorsmod.Wrapf(
			ibcerrors.ErrInvalidRequest,
			"inherited timeout timestamp is too far in the future: got %d, maximum %d",
			inheritedTimeoutTimestampNano,
			maxAcceptable,
		)
	}

	return nil
}
