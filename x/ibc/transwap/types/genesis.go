package types

import (
	"fmt"
	"strconv"

	host "github.com/cosmos/ibc-go/v11/modules/core/24-host"

	sdk "github.com/cosmos/cosmos-sdk/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
)

// NewGenesisState creates a new transwap GenesisState instance.
func NewGenesisState(portID string, denoms Denoms, totalEscrowed sdk.Coins) *transwapv1.GenesisState {
	return &transwapv1.GenesisState{
		PortId:        portID,
		Denoms:        denoms,
		TotalEscrowed: SDKCoinsToProto(totalEscrowed),
		Params:        DefaultParams(),
	}
}

// DefaultGenesisState returns a GenesisState with transwap as the default PortID.
func DefaultGenesisState() *transwapv1.GenesisState {
	return &transwapv1.GenesisState{
		PortId:        PortID,
		Denoms:        Denoms{},
		TotalEscrowed: SDKCoinsToProto(sdk.Coins{}),
		Params:        DefaultParams(),
		Refunds:       []*transwapv1.RefundRecord{},
	}
}

// ValidateGenesisState performs basic genesis state validation.
func ValidateGenesisState(gs *transwapv1.GenesisState) error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if err := host.PortIdentifierValidator(gs.GetPortId()); err != nil {
		return err
	}
	if err := Denoms(gs.GetDenoms()).Validate(); err != nil {
		return err
	}
	totalEscrowed, err := ProtoCoinsToSDK(gs.GetTotalEscrowed())
	if err != nil {
		return err
	}
	if err := totalEscrowed.Validate(); err != nil {
		return err
	}
	if err := ValidateParams(gs.GetParams()); err != nil {
		return err
	}
	refundIDs := make(map[string]struct{}, len(gs.GetRefunds()))
	livePackets := make(map[string]string, len(gs.GetRefunds()))
	for i, refund := range gs.GetRefunds() {
		if err := ValidateRefundRecord(refund); err != nil {
			return fmt.Errorf("invalid refund %d: %w", i, err)
		}
		if _, exists := refundIDs[refund.GetId()]; exists {
			return fmt.Errorf("duplicate refund id %q", refund.GetId())
		}
		refundIDs[refund.GetId()] = struct{}{}
		switch refund.GetStatus() {
		case transwapv1.RefundStatus_REFUND_STATUS_PENDING:
			key := refundPacketIdentity(
				refund.GetOriginalOutputPort(),
				refund.GetOriginalOutputChannel(),
				refund.GetOriginalOutputSequence(),
			)
			if existing, exists := livePackets[key]; exists {
				return fmt.Errorf("refunds %q and %q share live packet %s", existing, refund.GetId(), key)
			}
			livePackets[key] = refund.GetId()
		case transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT:
			key := refundPacketIdentity(
				refund.GetRefundSourcePort(),
				refund.GetRefundSourceChannel(),
				refund.GetActivePacketSequence(),
			)
			if existing, exists := livePackets[key]; exists {
				return fmt.Errorf("refunds %q and %q share live packet %s", existing, refund.GetId(), key)
			}
			livePackets[key] = refund.GetId()
		case transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE:
			if refund.GetNextRetryHeight() == 0 {
				return fmt.Errorf("retryable refund %q must be scheduled", refund.GetId())
			}
		}
	}
	return nil
}

func refundPacketIdentity(portID, channelID string, sequence uint64) string {
	return portID + "/" + channelID + "/" + strconv.FormatUint(sequence, 10)
}
