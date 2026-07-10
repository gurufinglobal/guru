package types

import (
	fmt "fmt"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"

	host "github.com/cosmos/ibc-go/v11/modules/core/24-host"

	errorsmod "cosmossdk.io/errors"
)

// NewHop creates a Hop with the given port ID and channel ID.
func NewHop(portID, channelID string) *transwapv1.Hop {
	return &transwapv1.Hop{PortId: portID, ChannelId: channelID}
}

// ValidateHop performs a basic validation of the Hop fields.
func ValidateHop(h *transwapv1.Hop) error {
	if h == nil {
		return errorsmod.Wrap(ErrInvalidDenomForTransfer, "hop cannot be nil")
	}

	if err := host.PortIdentifierValidator(h.PortId); err != nil {
		return errorsmod.Wrapf(err, "invalid hop source port ID %s", h.PortId)
	}
	if err := host.ChannelIdentifierValidator(h.ChannelId); err != nil {
		return errorsmod.Wrapf(err, "invalid hop source channel ID %s", h.ChannelId)
	}

	return nil
}

// HopPath returns the Hop in the format: <portID>/<channelID>.
func HopPath(h *transwapv1.Hop) string {
	if h == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", h.PortId, h.ChannelId)
}
