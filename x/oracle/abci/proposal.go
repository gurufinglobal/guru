package abci

import (
	"fmt"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
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

	// Oracle payloads are consensus data, not user transactions. The proposer
	// owns Txs[0] for this payload and strips any user-injected payload txs
	// before handing the remaining mempool txs to the normal proposal handler.
	innerReq := *req
	innerReq.Txs = stripPayloadTxs(req.Txs)
	payloadTx := []byte(nil)
	if payload != nil {
		payloadTx, err = EncodeProposalTx(payload)
		if err != nil {
			return nil, err
		}
		innerReq.MaxTxBytes = remainingMaxTxBytes(innerReq.MaxTxBytes, [][]byte{payloadTx})
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
	txs = append(txs, trimTxsToMaxBytes(stripPayloadTxs(resp.Txs), innerReq.MaxTxBytes)...)
	resp.Txs = txs
	return resp, nil
}

func (h ProposalHandler) ProcessProposal(ctx sdk.Context, req *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
	if containsPayloadAfterFirst(req.Txs) {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_REJECT}, nil
	}

	// Validators recompute whether a payload is expected and verify its exact
	// content before normal tx verification runs on the payload-stripped list.
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

func remainingMaxTxBytes(maxTxBytes int64, reservedTxs [][]byte) int64 {
	if maxTxBytes <= 0 {
		return 0
	}

	reservedBytes := proposalTxBytes(reservedTxs)
	if reservedBytes >= maxTxBytes {
		return 0
	}

	return maxTxBytes - reservedBytes
}

func trimTxsToMaxBytes(txs [][]byte, maxTxBytes int64) [][]byte {
	if maxTxBytes <= 0 {
		return nil
	}

	trimmed := make([][]byte, 0, len(txs))
	totalBytes := int64(0)
	for _, tx := range txs {
		txBytes := proposalTxBytes([][]byte{tx})
		if totalBytes+txBytes > maxTxBytes {
			continue
		}
		totalBytes += txBytes
		trimmed = append(trimmed, tx)
	}

	return trimmed
}

func proposalTxBytes(txs [][]byte) int64 {
	totalBytes := int64(0)
	for _, tx := range txs {
		totalBytes += cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{tx})
	}

	return totalBytes
}
