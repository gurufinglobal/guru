package abci

import (
	"context"
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
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

func TestProposalHelpersRejectAndStripMalformedOracleCandidates(t *testing.T) {
	payloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{Height: 2})
	require.NoError(t, err)
	malformedPayloadTx := mutateProposalTx(t, payloadTx, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
		body.Memo = "not canonical"
	})(t)
	normalTx := []byte("normal")

	require.True(t, containsPayloadAfterFirst([][]byte{normalTx, malformedPayloadTx}))
	require.Equal(t, [][]byte{normalTx}, stripPayloadTxs([][]byte{payloadTx, malformedPayloadTx, normalTx}))

	processCalled := false
	handler := NewProposalHandler(
		Aggregator{},
		nil,
		func(sdk.Context, *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
			processCalled = true
			return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
		},
	)
	resp, err := handler.ProcessProposal(sdk.Context{}, &abcitypes.RequestProcessProposal{
		Txs: [][]byte{malformedPayloadTx},
	})
	require.NoError(t, err)
	require.False(t, processCalled)
	require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, resp.Status)
}

func TestPayloadSkippingTxRunnerPreservesResultSlotsAndOriginalIndexes(t *testing.T) {
	payloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{Height: 2})
	require.NoError(t, err)

	runner := NewPayloadSkippingTxRunner(fakeRunner{})
	deliveredIndexes := make([]int, 0, 2)
	results, err := runner.Run(context.Background(), nil, [][]byte{
		payloadTx,
		[]byte("tx-a"),
		[]byte("tx-b"),
	}, func(_ []byte, _ sdk.Tx, _ storetypes.MultiStore, txIndex int, _ map[string]any) *abcitypes.ExecTxResult {
		deliveredIndexes = append(deliveredIndexes, txIndex)
		return &abcitypes.ExecTxResult{GasWanted: int64(txIndex)}
	})
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, deliveredIndexes)
	require.Len(t, results, 3)
	require.Equal(t, int64(0), results[0].GasWanted)
	require.Equal(t, int64(1), results[1].GasWanted)
	require.Equal(t, int64(2), results[2].GasWanted)
}

func TestPayloadSkippingTxRunnerSkipsOracleOnlyBlockWithoutCallingInner(t *testing.T) {
	payloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{Height: 2})
	require.NoError(t, err)

	innerCalls := 0
	deliverCalls := 0
	runner := NewPayloadSkippingTxRunner(countingRunner{calls: &innerCalls})
	results, err := runner.Run(context.Background(), nil, [][]byte{payloadTx}, func(
		[]byte,
		sdk.Tx,
		storetypes.MultiStore,
		int,
		map[string]any,
	) *abcitypes.ExecTxResult {
		deliverCalls++
		return &abcitypes.ExecTxResult{Code: 1}
	})
	require.NoError(t, err)
	require.Zero(t, innerCalls)
	require.Zero(t, deliverCalls)
	require.Len(t, results, 1)
	require.Zero(t, results[0].Code)
}

func TestPayloadSkippingTxRunnerNeverSkipsMalformedOrNonFirstRecords(t *testing.T) {
	payloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{Height: 2})
	require.NoError(t, err)
	malformedPayloadTx := mutateProposalTx(t, payloadTx, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
		body.Memo = "not canonical"
	})(t)

	deliveredIndexes := make([]int, 0, 3)
	runner := NewPayloadSkippingTxRunner(fakeRunner{})
	results, err := runner.Run(context.Background(), nil, [][]byte{
		malformedPayloadTx,
		payloadTx,
		[]byte("normal"),
	}, func(_ []byte, _ sdk.Tx, _ storetypes.MultiStore, txIndex int, _ map[string]any) *abcitypes.ExecTxResult {
		deliveredIndexes = append(deliveredIndexes, txIndex)
		return &abcitypes.ExecTxResult{GasWanted: int64(100 + txIndex)}
	})
	require.NoError(t, err)
	require.Equal(t, []int{0, 1, 2}, deliveredIndexes)
	require.Len(t, results, 3)
	require.Equal(t, int64(100), results[0].GasWanted)
	require.Equal(t, int64(101), results[1].GasWanted)
	require.Equal(t, int64(102), results[2].GasWanted)
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

type countingRunner struct {
	calls *int
}

func (r countingRunner) Run(
	ctx context.Context,
	ms storetypes.MultiStore,
	txs [][]byte,
	deliverTx sdk.DeliverTxFunc,
) ([]*abcitypes.ExecTxResult, error) {
	*r.calls++
	return fakeRunner{}.Run(ctx, ms, txs, deliverTx)
}
