package abci

import (
	"fmt"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
)

type ProposalHandler struct {
	aggregator Aggregator
	prepare    sdk.PrepareProposalHandler
	process    sdk.ProcessProposalHandler
}

func NewProposalHandler(
	aggregator Aggregator,
	prepare sdk.PrepareProposalHandler,
	process sdk.ProcessProposalHandler,
) ProposalHandler {
	return ProposalHandler{
		aggregator: aggregator,
		prepare:    prepare,
		process:    process,
	}
}

func (h ProposalHandler) PrepareProposal(ctx sdk.Context, req *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
	payload, err := h.aggregator.BuildPayload(ctx, req.Height, req.LocalLastCommit)
	if err != nil {
		return nil, err
	}

	innerReq := *req
	innerReq.Txs = stripPayloadTxs(req.Txs)
	payloadTx := []byte(nil)
	if payload != nil {
		payloadTx, err = EncodeProposalTx(payload)
		if err != nil {
			return nil, err
		}
		if innerReq.MaxTxBytes > int64(len(payloadTx)) {
			innerReq.MaxTxBytes -= int64(len(payloadTx))
		}
	}

	resp, err := h.prepare(ctx, &innerReq)
	if err != nil {
		return nil, err
	}
	if payloadTx == nil {
		return resp, nil
	}

	txs := make([][]byte, 0, len(resp.Txs)+1)
	txs = append(txs, payloadTx)
	txs = append(txs, stripPayloadTxs(resp.Txs)...)
	resp.Txs = txs
	return resp, nil
}

func (h ProposalHandler) ProcessProposal(ctx sdk.Context, req *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
	if containsPayloadAfterFirst(req.Txs) {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_REJECT}, nil
	}

	payload, hasPayload, err := firstPayload(req.Txs)
	if err != nil {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_REJECT}, nil
	}

	expected, err := h.aggregator.OraclePayloadExpected(ctx)
	if err != nil {
		return nil, err
	}
	if expected && !hasPayload {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_REJECT}, nil
	}
	if !expected && hasPayload {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_REJECT}, nil
	}
	if err := h.aggregator.VerifyPayload(ctx, payload); err != nil {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_REJECT}, nil
	}

	innerReq := *req
	innerReq.Txs = stripPayloadTxs(req.Txs)
	return h.process(ctx, &innerReq)
}

func (h ProposalHandler) ApplyProposalPayload(ctx sdk.Context, req *abcitypes.RequestFinalizeBlock) error {
	if containsPayloadAfterFirst(req.Txs) {
		return fmt.Errorf("oracle proposal payload is only allowed at Txs[0]")
	}

	payload, hasPayload, err := firstPayload(req.Txs)
	if err != nil {
		return err
	}
	expected, err := h.aggregator.OraclePayloadExpected(ctx)
	if err != nil {
		return err
	}
	if expected && !hasPayload {
		return fmt.Errorf("missing oracle proposal payload at height %d", ctx.BlockHeight())
	}
	if !expected && hasPayload {
		return fmt.Errorf("unexpected oracle proposal payload at height %d", ctx.BlockHeight())
	}

	return h.aggregator.ApplyPayload(ctx, payload)
}

func firstPayload(txs [][]byte) (payload *oraclev1.OracleProposalPayload, hasPayload bool, err error) {
	if len(txs) == 0 || !IsProposalTx(txs[0]) {
		return nil, false, nil
	}

	payload, _, err = DecodeProposalTx(txs[0])
	return payload, true, err
}
