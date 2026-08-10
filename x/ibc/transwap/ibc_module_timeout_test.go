package transwap

import (
	"testing"

	"github.com/stretchr/testify/require"

	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
)

func TestV1ExchangeSourceTimeoutTimestamp(t *testing.T) {
	packet := channeltypes.Packet{TimeoutTimestamp: 987654321}
	require.Equal(t, uint64(987654321), v1ExchangeSourceTimeoutTimestamp(packet))
}
