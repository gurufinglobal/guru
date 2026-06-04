package abci

import (
	"context"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type PayloadSkippingTxRunner struct {
	inner sdk.TxRunner
}

func NewPayloadSkippingTxRunner(inner sdk.TxRunner) PayloadSkippingTxRunner {
	return PayloadSkippingTxRunner{inner: inner}
}

func (r PayloadSkippingTxRunner) Run(
	ctx context.Context,
	ms storetypes.MultiStore,
	txs [][]byte,
	deliverTx sdk.DeliverTxFunc,
) ([]*abcitypes.ExecTxResult, error) {
	filteredTxs := make([][]byte, 0, len(txs))
	originalIndexes := make([]int, 0, len(txs))
	results := make([]*abcitypes.ExecTxResult, len(txs))

	for i, tx := range txs {
		if IsProposalTx(tx) {
			results[i] = &abcitypes.ExecTxResult{}
			continue
		}
		originalIndexes = append(originalIndexes, i)
		filteredTxs = append(filteredTxs, tx)
	}
	if len(filteredTxs) == 0 {
		return results, nil
	}

	filteredResults, err := r.inner.Run(ctx, ms, filteredTxs, func(tx []byte, memTx sdk.Tx, ms storetypes.MultiStore, txIndex int, incarnationCache map[string]any) *abcitypes.ExecTxResult {
		return deliverTx(tx, memTx, ms, originalIndexes[txIndex], incarnationCache)
	})
	if err != nil {
		return nil, err
	}
	for i, result := range filteredResults {
		results[originalIndexes[i]] = result
	}

	return results, nil
}
