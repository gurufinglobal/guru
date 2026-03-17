package cosmos

import (
	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	gurutypes "github.com/gurufinglobal/guru/v2/types"
)

const FixedMsgSendGas uint64 = 21_000

// FixedSendGasDecorator forces single MsgSend transactions to consume a fixed
// gas amount for both simulation and execution.
type FixedSendGasDecorator struct{}

func NewFixedSendGasDecorator() FixedSendGasDecorator {
	return FixedSendGasDecorator{}
}

func (d FixedSendGasDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	if !IsSingleMsgSendTx(tx) {
		return next(ctx, tx, simulate)
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(errortypes.ErrTxDecode, "Tx must be a FeeTx")
	}

	if !simulate && feeTx.GetGas() < FixedMsgSendGas {
		return ctx, errorsmod.Wrapf(
			errortypes.ErrOutOfGas,
			"single MsgSend tx requires minimum gas limit %d, got %d",
			FixedMsgSendGas,
			feeTx.GetGas(),
		)
	}

	fixedMeter := gurutypes.NewFixedGasMeter(storetypes.Gas(FixedMsgSendGas))
	return next(ctx.WithGasMeter(fixedMeter), tx, simulate)
}

// EffectiveGasWanted returns the gas used for fee checks and gas wanted
// aggregation. Single MsgSend txs are normalized to 21,000.
func EffectiveGasWanted(tx sdk.Tx, feeTx sdk.FeeTx) uint64 {
	if IsSingleMsgSendTx(tx) {
		return FixedMsgSendGas
	}
	return feeTx.GetGas()
}

// IsSingleMsgSendTx returns true when the tx contains exactly one message and
// the message type is bank MsgSend.
func IsSingleMsgSendTx(tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return false
	}

	switch msgs[0].(type) {
	case *banktypes.MsgSend:
		return true
	default:
		return false
	}
}
