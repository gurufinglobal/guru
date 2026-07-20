package abci

import (
	"context"
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestProcessProposalRejectsPayloadAfterFirst(t *testing.T) {
	payloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{Height: 2})
	require.NoError(t, err)

	handler := ProposalHandler{}
	resp, err := handler.ProcessProposal(sdk.Context{}, &abcitypes.RequestProcessProposal{
		Txs: [][]byte{[]byte("normal"), payloadTx},
	})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, resp.Status)
}

func TestPayloadSkippingTxRunnerPreservesResultSlotsAndOriginalIndexes(t *testing.T) {
	payloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{Height: 2})
	require.NoError(t, err)

	runner := NewPayloadSkippingTxRunner(fakeRunner{})
	results, err := runner.Run(context.Background(), nil, [][]byte{
		payloadTx,
		[]byte("tx-a"),
		[]byte("tx-b"),
	}, func(_ []byte, _ sdk.Tx, _ storetypes.MultiStore, txIndex int, _ map[string]any) *abcitypes.ExecTxResult {
		return &abcitypes.ExecTxResult{GasWanted: int64(txIndex)}
	})
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.Equal(t, int64(0), results[0].GasWanted)
	require.Equal(t, int64(1), results[1].GasWanted)
	require.Equal(t, int64(2), results[2].GasWanted)
}

type fakeRunner struct{}

func (fakeRunner) Run(
	_ context.Context,
	ms storetypes.MultiStore,
	txs [][]byte,
	deliverTx sdk.DeliverTxFunc,
) ([]*abcitypes.ExecTxResult, error) {
	results := make([]*abcitypes.ExecTxResult, 0, len(txs))
	for i, tx := range txs {
		results = append(results, deliverTx(tx, nil, ms, i, nil))
	}

	return results, nil
}
