package app

import (
	"fmt"
	"math"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	appante "github.com/gurufinglobal/guru/v2/app/ante"
)

// standardMsgSendProposalHandler preserves the SDK NoOp proposal semantics for
// ordinary transactions while enforcing consensus admission and G-based block
// accounting for bounded FixedSendGas transactions. Proposal admission uses the
// H-1 committed state, the target-height MGP already committed by H-1 EndBlock,
// the proposal-time projected BaseFee, and prior successful ante writes only.
// Target-height PreBlock, BeginBlock, and message effects are deliberately
// excluded. Oracle proposal records are handled by the outer Oracle proposal
// handler and never reach this handler.
type standardMsgSendProposalHandler struct {
	app        *App
	classifier appante.StandardMsgSendClassifier
}

func newStandardMsgSendProposalHandler(app *App) *standardMsgSendProposalHandler {
	return &standardMsgSendProposalHandler{
		app:        app,
		classifier: appante.NewStandardMsgSendClassifier(app.AccountKeeper),
	}
}

// PrepareProposal keeps CometBFT's input order and original transaction bytes.
// A proposer drops malformed or invalid FixedSendGas candidates locally. It
// never returns a per-transaction error because BaseApp falls back to the
// unfiltered request when a PrepareProposal handler returns an error.
func (h *standardMsgSendProposalHandler) PrepareProposal(
	ctx sdk.Context,
	req *abci.RequestPrepareProposal,
) (*abci.ResponsePrepareProposal, error) {
	if req == nil || req.MaxTxBytes <= 0 {
		return &abci.ResponsePrepareProposal{}, nil
	}

	maxBlockGas := proposalMaxBlockGas(ctx)
	selected := make([][]byte, 0, len(req.Txs))
	var totalBytes int64
	var totalGas uint64

	for _, txBytes := range req.Txs {
		tx, err := h.decodeTx(txBytes)
		if err != nil {
			continue
		}

		txCtx := ctx.WithTxBytes(txBytes)
		fixed, err := h.classifier.ClassifyProposal(txCtx, tx)
		if err != nil {
			continue
		}

		txSize := cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{txBytes})
		if !proposalBytesFit(totalBytes, txSize, req.MaxTxBytes) {
			continue
		}

		txGas, err := proposalAccountingGas(tx, fixed)
		if err != nil || !proposalGasFits(totalGas, txGas, maxBlockGas) {
			continue
		}

		// Run the exact production ante handler against the original bytes. An
		// ordinary ante error does not change NoOp inclusion semantics, while a
		// FixedSendGas admission error removes the candidate. Successful ante state
		// alone advances the ephemeral PrepareProposal snapshot.
		anteErr := h.runAnte(txCtx, tx)
		if fixed && anteErr != nil {
			continue
		}

		selected = append(selected, txBytes)
		totalBytes += txSize
		totalGas += txGas

		if totalBytes >= req.MaxTxBytes ||
			(maxBlockGas > 0 && totalGas >= maxBlockGas) {
			break
		}
	}

	return &abci.ResponsePrepareProposal{Txs: selected}, nil
}

// ProcessProposal rejects the complete proposal when any normal transaction is
// malformed, any FixedSendGas candidate fails classification or admission, or
// the mixed ordinary/FSG accounting gas exceeds the consensus block budget.
// Decode-valid ordinary ante errors retain the configured NoOp semantics.
func (h *standardMsgSendProposalHandler) ProcessProposal(
	ctx sdk.Context,
	req *abci.RequestProcessProposal,
) (*abci.ResponseProcessProposal, error) {
	if req == nil {
		return rejectStandardMsgSendProposal(), nil
	}

	maxBlockGas := proposalMaxBlockGas(ctx)
	var totalGas uint64

	for _, txBytes := range req.Txs {
		tx, err := h.decodeTx(txBytes)
		if err != nil {
			return rejectStandardMsgSendProposal(), nil
		}

		txCtx := ctx.WithTxBytes(txBytes)
		fixed, err := h.classifier.ClassifyProposal(txCtx, tx)
		if err != nil {
			return rejectStandardMsgSendProposal(), nil
		}

		txGas, err := proposalAccountingGas(tx, fixed)
		if err != nil || !proposalGasFits(totalGas, txGas, maxBlockGas) {
			return rejectStandardMsgSendProposal(), nil
		}
		totalGas += txGas

		// Successful ante writes advance the ephemeral proposal snapshot in tx
		// order. PreBlock, BeginBlock, and message effects are intentionally absent;
		// ordinary ante errors remain acceptable, while FixedSendGas ante errors
		// invalidate the proposal.
		anteErr := h.runAnte(txCtx, tx)
		if fixed && anteErr != nil {
			return rejectStandardMsgSendProposal(), nil
		}
	}

	return &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_ACCEPT,
	}, nil
}

func (h *standardMsgSendProposalHandler) decodeTx(txBytes []byte) (tx sdk.Tx, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			tx = nil
			err = fmt.Errorf("panic while decoding proposal transaction: %v", recovered)
		}
	}()
	tx, err = h.app.TxDecode(txBytes)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("proposal transaction decoder returned nil")
	}
	return tx, nil
}

// runAnte advances only successful ante writes in the non-persistent proposal
// snapshot. It intentionally does not execute messages or the target-height
// PreBlock/BeginBlock lifecycle; those effects belong exclusively to
// FinalizeBlock and any resulting message failure remains per-transaction.
func (h *standardMsgSendProposalHandler) runAnte(
	ctx sdk.Context,
	tx sdk.Tx,
) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic while validating proposal transaction: %v", recovered)
		}
	}()

	if h.app.anteHandler == nil {
		return fmt.Errorf("proposal ante handler is not configured")
	}
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return fmt.Errorf("proposal transaction must contain at least one message")
	}
	for _, msg := range msgs {
		if msg == nil {
			return fmt.Errorf("proposal transaction contains a nil message")
		}
		if basic, ok := msg.(sdk.HasValidateBasic); ok {
			if err := basic.ValidateBasic(); err != nil {
				return err
			}
		}
		if h.app.MsgServiceRouter().Handler(msg) == nil {
			return fmt.Errorf("no proposal message handler found for %T", msg)
		}
	}

	anteCtx, write := ctx.CacheContext()
	anteCtx = anteCtx.WithEventManager(sdk.NewEventManager())
	_, err = h.app.anteHandler(anteCtx, tx, false)
	if err != nil {
		return err
	}
	write()
	return nil
}

func proposalAccountingGas(tx sdk.Tx, fixed bool) (gas uint64, err error) {
	if fixed {
		return appante.StandardMsgSendGas, nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			gas = 0
			err = fmt.Errorf("panic while reading proposal transaction gas: %v", recovered)
		}
	}()
	if gasTx, ok := tx.(baseapp.GasTx); ok {
		return gasTx.GetGas(), nil
	}
	return 0, nil
}

func proposalMaxBlockGas(ctx sdk.Context) uint64 {
	block := ctx.ConsensusParams().Block
	if block == nil || block.MaxGas <= 0 {
		return 0
	}
	return uint64(block.MaxGas)
}

func proposalBytesFit(total, txSize, max int64) bool {
	return txSize >= 0 && total >= 0 && total <= max && txSize <= max-total
}

func proposalGasFits(total, txGas, max uint64) bool {
	if total > math.MaxUint64-txGas {
		return false
	}
	return max == 0 || (total <= max && txGas <= max-total)
}

func rejectStandardMsgSendProposal() *abci.ResponseProcessProposal {
	return &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_REJECT,
	}
}
