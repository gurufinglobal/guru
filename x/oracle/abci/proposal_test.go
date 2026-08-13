package abci

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
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
