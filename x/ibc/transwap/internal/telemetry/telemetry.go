package telemetry

import (
	"fmt"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"

	"github.com/hashicorp/go-metrics"

	coremetrics "github.com/cosmos/ibc-go/v11/modules/core/metrics"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func ReportTransfer(sourcePort, sourceChannel, destinationPort, destinationChannel string, token *transwapv1.Token) {
	labels := []metrics.Label{
		{Name: coremetrics.LabelDestinationPort, Value: destinationPort},
		{Name: coremetrics.LabelDestinationChannel, Value: destinationChannel},
	}

	if token == nil {
		return
	}
	amount, err := uint256decimal.ParseCanonicalPositive(token.Amount)
	if err == nil && amount.IsInt64() {
		metrics.SetGaugeWithLabels(
			[]string{"tx", "msg", "ibc", "transfer"},
			float32(amount.Int64()),
			[]metrics.Label{{Name: coremetrics.LabelDenom, Value: types.DenomPath(token.Denom)}},
		)
	}

	labels = append(labels, metrics.Label{Name: coremetrics.LabelSource, Value: fmt.Sprintf("%t", !types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel))})

	metrics.IncrCounterWithLabels(
		[]string{"ibc", types.ModuleName, "send"},
		1,
		labels,
	)
}

func ReportOnRecvPacket(sourcePort, sourceChannel, destinationPort, destinationChannel string, token *transwapv1.Token) {
	token = types.CloneToken(token)
	if token == nil {
		return
	}

	labels := []metrics.Label{
		{Name: coremetrics.LabelSourcePort, Value: sourcePort},
		{Name: coremetrics.LabelSourceChannel, Value: sourceChannel},
	}

	// Modify trace as Recv does.
	if types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel) {
		token.Denom.Trace = token.Denom.Trace[1:]
	} else {
		trace := []*transwapv1.Hop{types.NewHop(destinationPort, destinationChannel)}
		token.Denom.Trace = append(trace, token.Denom.Trace...)
	}

	// Transfer amount has already been parsed in caller.
	transferAmount, err := uint256decimal.ParseCanonicalPositive(token.Amount)
	if err == nil && transferAmount.IsInt64() {
		metrics.SetGaugeWithLabels(
			[]string{"ibc", types.ModuleName, "packet", "receive"},
			float32(transferAmount.Int64()),
			[]metrics.Label{{Name: coremetrics.LabelDenom, Value: types.DenomPath(token.Denom)}},
		)
	}

	labels = append(labels, metrics.Label{Name: coremetrics.LabelSource, Value: fmt.Sprintf("%t", types.DenomHasPrefix(token.Denom, sourcePort, sourceChannel))})

	metrics.IncrCounterWithLabels(
		[]string{"ibc", types.ModuleName, "receive"},
		1,
		labels,
	)
}
