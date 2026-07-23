package types

import (
	"bytes"
	"crypto/sha256"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/stretchr/testify/require"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
)

func TestValidateRefundRecordUsesAddressBytesAcrossBech32Prefixes(t *testing.T) {
	record := validRefundRecordForTest(t)
	require.NoError(t, ValidateRefundRecord(record))

	different := sdk.AccAddress(bytes.Repeat([]byte{0x24}, 20))
	record.ClaimAddress = different.String()
	require.ErrorIs(t, ValidateRefundRecord(record), ErrInvalidRefundState)
}

func TestValidateRefundRecordRequiresOriginalCommitmentAndCompleteActiveMetadata(t *testing.T) {
	valid := validRefundRecordForTest(t)

	tests := []struct {
		name   string
		mutate func(*RefundRecord)
	}{
		{
			name: "missing original commitment",
			mutate: func(record *RefundRecord) {
				record.OriginalOutputPacketCommitment = nil
			},
		},
		{
			name: "partial active packet",
			mutate: func(record *RefundRecord) {
				record.Status = RefundStatus_REFUND_STATUS_IN_FLIGHT
				record.RetryCount = 1
				record.ActivePacketSequence = 9
			},
		},
		{
			name: "missing original fee",
			mutate: func(record *RefundRecord) {
				record.OriginalFee = sdk.Coin{}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := CloneRefundRecord(valid)
			test.mutate(record)
			require.ErrorIs(t, ValidateRefundRecord(record), ErrInvalidRefundState)
		})
	}
}

func TestValidateGenesisRejectsCrossStateLivePacketCollision(t *testing.T) {
	pending := validRefundRecordForTest(t)
	active := CloneRefundRecord(pending)
	active.Id = RefundID(PortID, "channel-8", 13)
	active.Status = RefundStatus_REFUND_STATUS_IN_FLIGHT
	active.OriginalOutputChannel = "channel-8"
	active.OriginalOutputSequence = 13
	active.RefundSourceChannel = pending.GetOriginalOutputChannel()
	active.ActivePacketSequence = pending.GetOriginalOutputSequence()
	active.ActiveTimeoutTimestamp = pending.GetOriginalTimeoutTimestamp() + 1
	active.RetryCount = 1
	commitment := sha256.Sum256([]byte(active.GetId()))
	active.OriginalOutputPacketCommitment = commitment[:]

	genesis := DefaultGenesisState()
	genesis.Refunds = []*RefundRecord{pending, active}
	require.ErrorContains(t, ValidateGenesisState(genesis), "share live packet")
}

func TestValidateGenesisRequiresRetryableSchedule(t *testing.T) {
	retryable := validRefundRecordForTest(t)
	retryable.Status = RefundStatus_REFUND_STATUS_RETRYABLE
	retryable.RetryCount = 1
	genesis := DefaultGenesisState()
	genesis.Refunds = []*RefundRecord{retryable}
	require.ErrorContains(t, ValidateGenesisState(genesis), "must be scheduled")

	retryable.NextRetryHeight = 10
	require.NoError(t, ValidateGenesisState(genesis))
}

func TestValidateRefundRecordRejectsRetryCountAboveHardMaximum(t *testing.T) {
	record := validRefundRecordForTest(t)
	record.Status = RefundStatus_REFUND_STATUS_RETRYABLE
	record.RetryCount = MaximumMaxRefundRetries + 1
	record.NextRetryHeight = 10

	err := ValidateRefundRecord(record)
	require.ErrorIs(t, err, ErrInvalidRefundState)
	require.ErrorContains(t, err, "retry count exceeds hard maximum")
}

func TestValidateRefundRecordRejectsOnlyNonMaterializableLocalDenom(t *testing.T) {
	invalidNative := validRefundRecordForTest(t)
	invalidNative.Token.Denom = NewDenom("!")
	err := ValidateRefundRecord(invalidNative)
	require.ErrorIs(t, err, ErrInvalidRefundState)
	require.ErrorContains(t, err, "cannot be materialized as a local bank coin")

	tracedRemote := validRefundRecordForTest(t)
	tracedRemote.Token.Denom = NewDenom("!", NewHop(PortID, "channel-0"))
	localIBCDenom := DenomIBCDenom(tracedRemote.Token.Denom)
	tracedRemote.OriginalFee = SDKCoinToProto(sdk.NewInt64Coin(localIBCDenom, 1))
	require.NoError(t, ValidateRefundRecord(tracedRemote))
}

func validRefundRecordForTest(t *testing.T) *RefundRecord {
	t.Helper()
	address := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))
	remoteReceiver, err := bech32.ConvertAndEncode("remote", address)
	require.NoError(t, err)
	commitment := sha256.Sum256([]byte("original-output"))

	return &RefundRecord{
		Id:                             RefundID(PortID, "channel-7", 12),
		Status:                         RefundStatus_REFUND_STATUS_PENDING,
		RefundSourcePort:               PortID,
		RefundSourceChannel:            "channel-0",
		Token:                          Token{Denom: NewDenom("uatom"), Amount: "100"},
		Receiver:                       remoteReceiver,
		ClaimAddress:                   address.String(),
		ExchangeId:                     "7",
		OriginalFee:                    SDKCoinToProto(sdk.NewInt64Coin("uatom", 1)),
		OriginalTimeoutTimestamp:       1_700_000_000_000_000_000,
		OriginalTimeoutHeight:          &RefundHeight{RevisionNumber: 2, RevisionHeight: 99},
		OriginalOutputPort:             PortID,
		OriginalOutputChannel:          "channel-7",
		OriginalOutputSequence:         12,
		OriginalOutputPacketCommitment: commitment[:],
		VolumeReservation: &bextypes.VolumeReservation{
			ExchangeId:             7,
			Direction:              bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
			EpochSeconds:           bextypes.MinVolumeEpochSeconds,
			Amount:                 "100",
			VolumeWindowGeneration: 1,
		},
	}
}
