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
	scenario := setupRefundStateMachineScenario(t)
	before := mustRefundRecord(t, scenario.state, scenario.refundID)
	sentCount := scenario.state.ics4.sentCount(scenario.state.ctx)
	originalOutput := refundSentPacketData(t, scenario.state, exchangeAtomicSequence)

	ack := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected packet"))
	require.NoError(t, scenario.state.keeper.OnAcknowledgementTransferPacket(
		scenario.state.ctx,
		transtypes.PortID,
		"channel-7",
		exchangeAtomicSequence,
		originalOutput,
		ack,
	))

	require.Equal(t, sentCount, scenario.state.ics4.sentCount(scenario.state.ctx))
	require.Equal(t, before, mustRefundRecord(t, scenario.state, scenario.refundID))
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))
}
