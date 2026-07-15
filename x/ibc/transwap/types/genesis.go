package types

import (
	"fmt"
	"strconv"
	"strings"

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
	}
}

// DefaultGenesisState returns a GenesisState with transwap as the default PortID.
func DefaultGenesisState() *transwapv1.GenesisState {
	return &transwapv1.GenesisState{
		PortId:        PortID,
		Denoms:        Denoms{},
		TotalEscrowed: SDKCoinsToProto(sdk.Coins{}),
	}
}

// ValidateGenesisState performs basic genesis state validation.
func ValidateGenesisState(gs *transwapv1.GenesisState) error {
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
	seen := make(map[string]struct{}, len(gs.GetPendingRefunds()))
	for i, pending := range gs.GetPendingRefunds() {
		if pending == nil || pending.GetPacket() == nil {
			return fmt.Errorf("pending refund %d cannot be nil", i)
		}
		if err := validateRefundPacketKey(pending.GetKey()); err != nil {
			return fmt.Errorf("invalid pending refund key %d: %w", i, err)
		}
		if _, ok := seen[pending.GetKey()]; ok {
			return fmt.Errorf("duplicate pending refund key %q", pending.GetKey())
		}
		seen[pending.GetKey()] = struct{}{}
		packet := pending.GetPacket()
		if packet.GetSourcePort() != PortID {
			return fmt.Errorf("pending refund %q source port must be %s", pending.GetKey(), PortID)
		}
		if err := host.ChannelIdentifierValidator(packet.GetSourceChannel()); err != nil {
			return fmt.Errorf("pending refund %q has invalid source channel: %w", pending.GetKey(), err)
		}
		if err := ValidateToken(packet.GetToken()); err != nil {
			return fmt.Errorf("pending refund %q has invalid token: %w", pending.GetKey(), err)
		}
		if strings.TrimSpace(packet.GetSender()) == "" || strings.TrimSpace(packet.GetReceiver()) == "" {
			return fmt.Errorf("pending refund %q sender and receiver must be non-empty", pending.GetKey())
		}
		if packet.GetTimeoutTimestamp() == 0 || packet.GetOriginalTimeoutTimestamp() == 0 {
			return fmt.Errorf("pending refund %q timeout timestamps must be non-zero", pending.GetKey())
		}
		if exchangeID, err := strconv.ParseUint(packet.GetExchangeId(), 10, 64); err != nil || exchangeID == 0 {
			return fmt.Errorf("pending refund %q has invalid exchange id", pending.GetKey())
		}
		fee, err := ProtoCoinToSDK(packet.GetFee())
		if err != nil || fee.IsNegative() {
			return fmt.Errorf("pending refund %q has invalid fee", pending.GetKey())
		}
		liability, err := ProtoCoinToSDK(packet.GetPendingLiability())
		if err != nil || !liability.IsPositive() {
			return fmt.Errorf("pending refund %q has invalid pending liability", pending.GetKey())
		}
	}
	return nil
}

func validateRefundPacketKey(key string) error {
	parts := strings.Split(key, "/")
	if len(parts) != 4 || parts[0]+"/" != RefundPacketPrefix {
		return fmt.Errorf("expected refund/<port>/<channel>/<sequence>")
	}
	if err := host.PortIdentifierValidator(parts[1]); err != nil {
		return err
	}
	if err := host.ChannelIdentifierValidator(parts[2]); err != nil {
		return err
	}
	sequence, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil || sequence == 0 {
		return fmt.Errorf("sequence must be a positive uint64")
	}
	return nil
}
