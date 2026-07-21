package types

import (
	fmt "fmt"

	host "github.com/cosmos/ibc-go/v11/modules/core/24-host"

	errorsmod "cosmossdk.io/errors"
)

// NewHop creates a Hop with the given port ID and channel ID.
func NewHop(portID, channelID string) Hop {
	return Hop{PortId: portID, ChannelId: channelID}
}

// ValidateHop performs a basic validation of the Hop fields.
func ValidateHop(h Hop) error {
	if err := host.PortIdentifierValidator(h.PortId); err != nil {
		return errorsmod.Wrapf(err, "invalid hop source port ID %s", h.PortId)
	}
	if err := host.ChannelIdentifierValidator(h.ChannelId); err != nil {
		return errorsmod.Wrapf(err, "invalid hop source channel ID %s", h.ChannelId)
	}

	return nil
}

// HopPath returns the Hop in the format: <portID>/<channelID>.
func HopPath(h Hop) string {
	return h.String()
}

// String supplies the stringer deliberately disabled in token.proto and is
// also required for Hop to satisfy the gogo proto message contract.
func (h Hop) String() string {
	return fmt.Sprintf("%s/%s", h.PortId, h.ChannelId)
}
