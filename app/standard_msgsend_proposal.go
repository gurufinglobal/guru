package app

import (
	"context"
	"math"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	appante "github.com/gurufinglobal/guru/v3/app/ante"
)

// standardMsgSendTxSelector preserves the SDK byte-selection behavior while
// reserving the public 21k standardized gas value for every
// transaction-local candidate.
type standardMsgSendTxSelector struct {
	totalTxBytes uint64
	totalTxGas   uint64
	selectedTxs  [][]byte
}

var _ baseapp.TxSelector = (*standardMsgSendTxSelector)(nil)

func newStandardMsgSendTxSelector() baseapp.TxSelector {
	return &standardMsgSendTxSelector{}
}

func (s *standardMsgSendTxSelector) SelectedTxs(context.Context) [][]byte {
	selected := make([][]byte, len(s.selectedTxs))
	copy(selected, s.selectedTxs)
	return selected
}

func (s *standardMsgSendTxSelector) Clear() {
	s.totalTxBytes = 0
	s.totalTxGas = 0
	s.selectedTxs = nil
}

func (s *standardMsgSendTxSelector) SelectTxForProposal(
	_ context.Context,
	maxTxBytes uint64,
	maxBlockGas uint64,
	tx sdk.Tx,
	txBytes []byte,
) bool {
	txSize := uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{txBytes}))
	gas := standardMsgSendProposalGas(tx, txBytes)

	bytesFit := s.totalTxBytes <= maxTxBytes && txSize <= maxTxBytes-s.totalTxBytes
	finiteConsensusGas := maxBlockGas > 0 && maxBlockGas <= math.MaxInt64
	gasFit := true
	if finiteConsensusGas {
		gasFit = s.totalTxGas <= maxBlockGas && gas <= maxBlockGas-s.totalTxGas
	}
	if bytesFit && gasFit {
		s.totalTxBytes += txSize
		if gas <= math.MaxUint64-s.totalTxGas {
			s.totalTxGas += gas
		} else {
			s.totalTxGas = math.MaxUint64
		}
		s.selectedTxs = append(s.selectedTxs, txBytes)
	}

	bytesFull := s.totalTxBytes >= maxTxBytes
	gasFull := finiteConsensusGas && s.totalTxGas >= maxBlockGas
	return bytesFull || gasFull
}

func standardMsgSendProposalGas(tx sdk.Tx, txBytes []byte) uint64 {
	if appante.IsStandardMsgSendGasCandidate(tx, txBytes) {
		return appante.StandardMsgSendGas
	}
	if gasTx, ok := tx.(interface{ GetGas() uint64 }); ok {
		return gasTx.GetGas()
	}
	return 0
}

// standardMsgSendProcessProposal applies the standard 21k weight only when the
// tx shape is recognized; ordinary txs keep their declared gas.
func (app *App) standardMsgSendProcessProposal(next sdk.ProcessProposalHandler) sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		blockParams := ctx.ConsensusParams().Block
		finiteConsensusGas := blockParams != nil && blockParams.MaxGas > 0
		maxGas := uint64(0)
		if finiteConsensusGas {
			maxGas = uint64(blockParams.MaxGas)
		}
		var totalGas uint64
		for _, txBytes := range req.Txs {
			tx, err := app.txConfig.TxDecoder()(txBytes)
			if err != nil {
				return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
			}
			if !finiteConsensusGas {
				continue
			}
			gas := standardMsgSendProposalGas(tx, txBytes)
			if totalGas > maxGas || gas > maxGas-totalGas {
				return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
			}
			totalGas += gas
		}

		return next(ctx, req)
	}
}
