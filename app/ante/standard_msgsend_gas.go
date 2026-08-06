package ante

import (
	"bytes"
	"fmt"

	errorsmod "cosmossdk.io/errors"

	antetypes "github.com/cosmos/evm/ante/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	// StandardMsgSendGas is the public gas value for the bounded MsgSend shape.
	StandardMsgSendGas uint64 = 21_000

	// StandardMsgSendMaxMemoBytes retains the current default memo envelope as
	// an immutable bound for this class even if the auth parameter is raised.
	StandardMsgSendMaxMemoBytes = 256
)

// StandardMsgSendGasDecorator projects a deliberately narrow MsgSend shape to
// 21k public gas while retaining internal execution accounting. Every shape
// outside this class continues through the ordinary Cosmos gas path.
type StandardMsgSendGasDecorator struct {
	accountKeeper authante.AccountKeeper
}

func NewStandardMsgSendGasDecorator(
	accountKeeper authante.AccountKeeper,
) StandardMsgSendGasDecorator {
	return StandardMsgSendGasDecorator{
		accountKeeper: accountKeeper,
	}
}

func (d StandardMsgSendGasDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (newCtx sdk.Context, err error) {
	baseFeeTx, msg, isStandardClass := standardMsgSendShapeWithoutGasLimit(tx, ctx.TxBytes())
	if !isStandardClass {
		return next(ctx, tx, simulate)
	}
	baseGas := baseFeeTx.GetGas()
	if simulate {
		if baseGas != 0 && baseGas < StandardMsgSendGas {
			return next(ctx, tx, simulate)
		}
	} else if baseGas < StandardMsgSendGas {
		return ctx, errorsmod.Wrapf(
			sdkerrors.ErrInvalidGasLimit,
			"standard MsgSend requires at least %d gas",
			StandardMsgSendGas,
		)
	}

	if d.accountKeeper == nil {
		return ctx, errorsmod.Wrap(sdkerrors.ErrLogic, "standard MsgSend gas dependencies are not configured")
	}

	feePayer := baseFeeTx.FeePayer()
	from, err := d.accountKeeper.AddressCodec().StringToBytes(msg.FromAddress)
	if err != nil || !bytes.Equal(from, feePayer) || !d.hasBoundedSingleSigner(ctx, tx, feePayer) {
		return next(ctx, tx, simulate)
	}

	actualMeter := storetypes.NewInfiniteGasMeter()
	consumed := ctx.GasMeter().GasConsumed()
	if consumed > 0 {
		actualMeter.ConsumeGas(consumed, "standard MsgSend pre-classification gas")
	}

	standardCtx := ctx.WithGasMeter(newStandardMsgSendGasMeter(actualMeter))

	return next(standardCtx, tx, simulate)
}

// IsStandardMsgSendGasCandidate identifies the transaction-local superset that
// must receive the standardized 21k weight during proposal admission. Signer/account
// checks remain in the ante decorator; charging a larger superset is safe.
func IsStandardMsgSendGasCandidate(tx sdk.Tx, txBytes []byte) bool {
	_, _, eligible := standardMsgSendShape(tx, txBytes, false)
	return eligible
}

// standardMsgSendShape is intentionally transaction-local. Eligible execution
// still receives standard gas projection while remaining compatible with fee policy.
func standardMsgSendShape(
	tx sdk.Tx,
	txBytes []byte,
	simulate bool,
) (sdk.FeeTx, *banktypes.MsgSend, bool) {
	if tx == nil {
		return nil, nil, false
	}

	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil, nil, false
	}
	msg, ok := msgs[0].(*banktypes.MsgSend)
	if !ok || msg == nil || len(msg.Amount) != 1 {
		return nil, nil, false
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok || len(feeTx.FeeGranter()) != 0 || len(feeTx.GetFee()) > 1 {
		return nil, nil, false
	}
	if len(feeTx.GetFee()) == 1 && feeTx.GetFee()[0].Denom != evmtypes.GetEVMCoinDenom() {
		return nil, nil, false
	}
	declaredGas := feeTx.GetGas()
	if declaredGas < StandardMsgSendGas && !(simulate && declaredGas == 0) {
		return nil, nil, false
	}

	if memoTx, ok := tx.(sdk.TxWithMemo); ok && len(memoTx.GetMemo()) > StandardMsgSendMaxMemoBytes {
		return nil, nil, false
	}
	if unorderedTx, ok := tx.(sdk.TxWithUnordered); ok && unorderedTx.GetUnordered() {
		return nil, nil, false
	}

	if extensionTx, ok := tx.(authante.HasExtensionOptionsTx); ok {
		if len(extensionTx.GetNonCriticalExtensionOptions()) != 0 {
			return nil, nil, false
		}
		extensions := extensionTx.GetExtensionOptions()
		if len(extensions) > 1 {
			return nil, nil, false
		}
		if len(extensions) == 1 {
			if _, ok := extensions[0].GetCachedValue().(*antetypes.ExtensionOptionDynamicFeeTx); !ok {
				return nil, nil, false
			}
		}
	}

	return feeTx, msg, true
}

func standardMsgSendShapeWithoutGasLimit(
	tx sdk.Tx,
	txBytes []byte,
) (sdk.FeeTx, *banktypes.MsgSend, bool) {
	if tx == nil {
		return nil, nil, false
	}

	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil, nil, false
	}
	msg, ok := msgs[0].(*banktypes.MsgSend)
	if !ok || msg == nil || len(msg.Amount) != 1 {
		return nil, nil, false
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok || len(feeTx.FeeGranter()) != 0 || len(feeTx.GetFee()) > 1 {
		return nil, nil, false
	}
	if len(feeTx.GetFee()) == 1 && feeTx.GetFee()[0].Denom != evmtypes.GetEVMCoinDenom() {
		return nil, nil, false
	}

	if memoTx, ok := tx.(sdk.TxWithMemo); ok && len(memoTx.GetMemo()) > StandardMsgSendMaxMemoBytes {
		return nil, nil, false
	}
	if unorderedTx, ok := tx.(sdk.TxWithUnordered); ok && unorderedTx.GetUnordered() {
		return nil, nil, false
	}

	if extensionTx, ok := tx.(authante.HasExtensionOptionsTx); ok {
		if len(extensionTx.GetNonCriticalExtensionOptions()) != 0 {
			return nil, nil, false
		}
		extensions := extensionTx.GetExtensionOptions()
		if len(extensions) > 1 {
			return nil, nil, false
		}
		if len(extensions) == 1 {
			if _, ok := extensions[0].GetCachedValue().(*antetypes.ExtensionOptionDynamicFeeTx); !ok {
				return nil, nil, false
			}
		}
	}

	return feeTx, msg, true
}

func (d StandardMsgSendGasDecorator) hasBoundedSingleSigner(
	ctx sdk.Context,
	tx sdk.Tx,
	feePayer sdk.AccAddress,
) bool {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return false
	}
	signers, err := sigTx.GetSigners()
	if err != nil || len(signers) != 1 || !bytes.Equal(signers[0], feePayer) {
		return false
	}
	signatures, err := sigTx.GetSignaturesV2()
	if err != nil || len(signatures) != 1 {
		return false
	}
	pubKeys, err := sigTx.GetPubKeys()
	if err != nil || len(pubKeys) != 1 {
		return false
	}
	pubKey := pubKeys[0]
	if pubKey == nil {
		account := d.accountKeeper.GetAccount(ctx, feePayer)
		if account == nil {
			return false
		}
		pubKey = account.GetPubKey()
	}
	return hasBoundedStandardMsgSendPubKey(pubKey)
}

func hasBoundedStandardMsgSendPubKey(root cryptotypes.PubKey) bool {
	if root == nil {
		return false
	}

	switch root.(type) {
	case *secp256k1.PubKey, *ethsecp256k1.PubKey:
		return true
	default:
		return false
	}
}

type standardMsgSendGasMeter struct {
	actual storetypes.GasMeter
}

func newStandardMsgSendGasMeter(actual storetypes.GasMeter) storetypes.GasMeter {
	return &standardMsgSendGasMeter{actual: actual}
}

func (m *standardMsgSendGasMeter) GasConsumed() storetypes.Gas {
	return StandardMsgSendGas
}

func (m *standardMsgSendGasMeter) GasConsumedToLimit() storetypes.Gas {
	return StandardMsgSendGas
}

func (m *standardMsgSendGasMeter) GasRemaining() storetypes.Gas {
	return m.actual.GasRemaining()
}

func (m *standardMsgSendGasMeter) Limit() storetypes.Gas {
	return StandardMsgSendGas
}

func (m *standardMsgSendGasMeter) ConsumeGas(amount storetypes.Gas, descriptor string) {
	m.actual.ConsumeGas(amount, descriptor)
}

func (m *standardMsgSendGasMeter) RefundGas(amount storetypes.Gas, descriptor string) {
	m.actual.RefundGas(amount, descriptor)
}

func (m *standardMsgSendGasMeter) IsPastLimit() bool {
	return m.actual.IsPastLimit()
}

func (m *standardMsgSendGasMeter) IsOutOfGas() bool {
	return m.actual.IsOutOfGas()
}

func (m *standardMsgSendGasMeter) String() string {
	return fmt.Sprintf(
		"StandardMsgSendGasMeter{public: %d, actual: %s}",
		StandardMsgSendGas,
		m.actual.String(),
	)
}

func (m *standardMsgSendGasMeter) actualGasConsumed() storetypes.Gas {
	return m.actual.GasConsumed()
}
