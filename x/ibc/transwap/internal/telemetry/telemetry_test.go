package telemetry

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestTelemetryReportTransferRecordsMetrics(t *testing.T) {
	t.Run("int64 amount", func(t *testing.T) {
		require.NotPanics(t, func() {
			ReportTransfer("port", "channel-1", "dest", "channel-2", &types.Token{
				Denom:  types.NewDenom("uatom"),
				Amount: "123",
			})
		})
	})

	t.Run("large amount skips gauge path", func(t *testing.T) {
		hugeAmount := new(big.Int).SetUint64(1)
		hugeAmount.Lsh(hugeAmount, 100)
		require.NotPanics(t, func() {
			ReportTransfer("port", "channel-1", "dest", "channel-2", &types.Token{
				Denom:  types.NewDenom("uatom"),
				Amount: hugeAmount.String(),
			})
		})
	})

	t.Run("invalid amount skips gauge path", func(t *testing.T) {
		require.NotPanics(t, func() {
			ReportTransfer("port", "channel-1", "dest", "channel-2", &types.Token{
				Denom:  types.NewDenom("uatom"),
				Amount: "invalid-amount",
			})
		})
	})
}

func TestTelemetryReportOnRecvPacketRecordsMetrics(t *testing.T) {
	t.Run("prefixed incoming token", func(t *testing.T) {
		require.NotPanics(t, func() {
			ReportOnRecvPacket("port", "channel-1", "dest", "channel-2", &types.Token{
				Denom:  types.NewDenom("uatom", types.NewHop("port", "channel-1")),
				Amount: "456",
			})
		})
	})

	t.Run("non prefixed incoming token", func(t *testing.T) {
		require.NotPanics(t, func() {
			ReportOnRecvPacket("port", "channel-1", "dest", "channel-2", &types.Token{
				Denom:  types.NewDenom("uatom"),
				Amount: "456",
			})
		})
	})
}
