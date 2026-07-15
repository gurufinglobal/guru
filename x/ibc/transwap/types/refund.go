package types

import (
	"bytes"
	"crypto/sha256"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	host "github.com/cosmos/ibc-go/v11/modules/core/24-host"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
)

func RefundID(outputPort, outputChannel string, outputSequence uint64) string {
	return outputPort + "/" + outputChannel + "/" + strconv.FormatUint(outputSequence, 10)
}

func ValidateRefundRecord(record *transwapv1.RefundRecord) error {
	if record == nil {
		return ErrInvalidRefundState.Wrap("refund record cannot be nil")
	}
	if err := ValidateRefundID(record.GetId()); err != nil {
		return err
	}
	if record.GetStatus() == transwapv1.RefundStatus_REFUND_STATUS_UNSPECIFIED {
		return ErrInvalidRefundState.Wrap("refund status cannot be unspecified")
	}
	if record.GetRefundSourcePort() != PortID {
		return ErrInvalidRefundState.Wrapf("refund source port must be %s", PortID)
	}
	if err := host.ChannelIdentifierValidator(record.GetRefundSourceChannel()); err != nil {
		return ErrInvalidRefundState.Wrapf("invalid refund source channel: %v", err)
	}
	if err := ValidateToken(record.GetToken()); err != nil {
		return ErrInvalidRefundState.Wrapf("invalid refund token: %v", err)
	}
	_, receiverAddress, err := bech32.DecodeAndConvert(record.GetReceiver())
	if err != nil || sdk.VerifyAddressFormat(receiverAddress) != nil {
		return ErrInvalidRefundState.Wrap("invalid refund receiver")
	}
	claimAddress, err := sdk.AccAddressFromBech32(record.GetClaimAddress())
	if err != nil || len(claimAddress) == 0 {
		return ErrInvalidRefundState.Wrap("invalid claim address")
	}
	if !bytes.Equal(receiverAddress, claimAddress) {
		return ErrInvalidRefundState.Wrap("refund receiver and claim address must identify the same account")
	}
	kind, _, err := ClassifyExchangeID(record.GetExchangeId())
	if err != nil || kind != PacketKindExchange {
		return ErrInvalidRefundState.Wrap("invalid exchange id")
	}
	exchangeID, err := strconv.ParseUint(record.GetExchangeId(), 10, 64)
	if err != nil || exchangeID == 0 {
		return ErrInvalidRefundState.Wrap("invalid exchange id")
	}
	if _, err := bextypes.ValidateVolumeReservation(record.GetVolumeReservation()); err != nil {
		return ErrInvalidRefundState.Wrapf("invalid volume reservation: %v", err)
	}
	if record.GetVolumeReservation().GetExchangeId() != exchangeID {
		return ErrInvalidRefundState.Wrap("volume reservation exchange does not match refund")
	}
	tokenCoin, err := TokenToCoin(record.GetToken())
	if err != nil {
		return ErrInvalidRefundState.Wrapf("invalid refund token coin: %v", err)
	}
	fee, err := ProtoCoinToSDK(record.GetOriginalFee())
	if err != nil || fee.IsNegative() || fee.Denom != tokenCoin.Denom || !tokenCoin.Amount.GT(fee.Amount) {
		return ErrInvalidRefundState.Wrap("invalid original fee")
	}
	originalHeight := record.GetOriginalTimeoutHeight()
	if record.GetOriginalTimeoutTimestamp() == 0 &&
		(originalHeight == nil || originalHeight.GetRevisionHeight() == 0) {
		return ErrInvalidRefundState.Wrap("original timeout timestamp or height must be positive")
	}
	if record.GetOriginalOutputPort() != PortID {
		return ErrInvalidRefundState.Wrapf("original output port must be %s", PortID)
	}
	if err := host.ChannelIdentifierValidator(record.GetOriginalOutputChannel()); err != nil {
		return ErrInvalidRefundState.Wrapf("invalid original output channel: %v", err)
	}
	if record.GetOriginalOutputSequence() == 0 ||
		record.GetId() != RefundID(record.GetOriginalOutputPort(), record.GetOriginalOutputChannel(), record.GetOriginalOutputSequence()) {
		return ErrInvalidRefundState.Wrap("refund id must match the original output packet")
	}
	if len(record.GetOriginalOutputPacketCommitment()) != sha256.Size {
		return ErrInvalidRefundState.Wrap("original output packet commitment must be a sha256 hash")
	}
	if record.GetRetryCount() > MaximumMaxRefundRetries {
		return ErrInvalidRefundState.Wrapf("retry count exceeds hard maximum %d", MaximumMaxRefundRetries)
	}

	if HasPartialActiveRefundPacket(record) {
		return ErrInvalidRefundState.Wrap("active refund packet sequence and timeout must both be set or both be zero")
	}
	if record.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE && record.GetNextRetryHeight() != 0 {
		return ErrInvalidRefundState.Wrap("only retryable refunds may have a next retry height")
	}

	switch record.GetStatus() {
	case transwapv1.RefundStatus_REFUND_STATUS_PENDING:
		if record.GetRetryCount() != 0 || HasActiveRefundPacket(record) {
			return ErrInvalidRefundState.Wrap("pending refund has invalid retry or packet state")
		}
	case transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT:
		if record.GetRetryCount() == 0 || !HasActiveRefundPacket(record) {
			return ErrInvalidRefundState.Wrap("in-flight refund requires one active packet")
		}
	case transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE:
		if HasActiveRefundPacket(record) {
			return ErrInvalidRefundState.Wrap("retryable refund cannot have an active packet")
		}
	case transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE:
		if HasActiveRefundPacket(record) {
			return ErrInvalidRefundState.Wrap("manual refund cannot have an active packet")
		}
	case transwapv1.RefundStatus_REFUND_STATUS_COMPLETED:
		if HasActiveRefundPacket(record) {
			return ErrInvalidRefundState.Wrap("completed refund cannot have an active packet")
		}
	case transwapv1.RefundStatus_REFUND_STATUS_CLAIMED:
		if HasActiveRefundPacket(record) {
			return ErrInvalidRefundState.Wrap("claimed refund cannot have an active packet")
		}
	default:
		return ErrInvalidRefundState.Wrapf("unsupported refund status %s", record.GetStatus())
	}
	return nil
}

func ValidateRefundID(refundID string) error {
	parts := strings.Split(refundID, "/")
	if len(parts) != 3 {
		return ErrInvalidRefundState.Wrap("refund id must be <port>/<channel>/<sequence>")
	}
	if err := host.PortIdentifierValidator(parts[0]); err != nil {
		return ErrInvalidRefundState.Wrapf("invalid refund id port: %v", err)
	}
	if err := host.ChannelIdentifierValidator(parts[1]); err != nil {
		return ErrInvalidRefundState.Wrapf("invalid refund id channel: %v", err)
	}
	sequence, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil || sequence == 0 {
		return ErrInvalidRefundState.Wrap("invalid refund id sequence")
	}
	return nil
}

func HasActiveRefundPacket(record *transwapv1.RefundRecord) bool {
	return record.GetActivePacketSequence() != 0 && record.GetActiveTimeoutTimestamp() != 0
}

func HasPartialActiveRefundPacket(record *transwapv1.RefundRecord) bool {
	return (record.GetActivePacketSequence() == 0) != (record.GetActiveTimeoutTimestamp() == 0)
}
