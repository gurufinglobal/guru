//go:build redteam

package keeper

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"

	transtypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestRedteamDuplicateOriginalErrorAckIsGuardedIfAppCallbackReentered(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	ack := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected packet"))
	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		ack,
	))
	requireExchangeRefundCallbackState(t, state)

	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		ack,
	))

	requireExchangeRefundCallbackState(t, state)
}
