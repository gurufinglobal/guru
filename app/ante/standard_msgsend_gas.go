package ante

import (
	"bytes"
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"

	antetypes "github.com/cosmos/evm/ante/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

const (
	// StandardMsgSendGas is the consensus accounting gas for every eligible
	// FixedSendGas transaction. The signed gas limit remains unchanged in the
	// transaction bytes and is used only as the maximum fee-price denominator.
	StandardMsgSendGas uint64 = 21_000

	// StandardMsgSendMaxTxBytes is the FixedSendGas eligibility ceiling. It is
	// intentionally not a chain-wide transaction-size limit: larger decode-valid
	// transactions use the ordinary upstream path.
	StandardMsgSendMaxTxBytes = 2_048
)

type standardMsgSendContextKey struct{}

type standardMsgSendClassification struct {
	feeTx  sdk.FeeTx
	msg    *banktypes.MsgSend
	sender sdk.AccAddress
}

// StandardMsgSendClassifier is the single FixedSendGas classification source
// shared by ante and proposal handling. A nil error with eligible=false means
// ordinary upstream fallback; an error is a deterministic admission failure.
type StandardMsgSendClassifier struct {
	accountKeeper authante.AccountKeeper
}

func NewStandardMsgSendClassifier(accountKeeper authante.AccountKeeper) StandardMsgSendClassifier {
	return StandardMsgSendClassifier{accountKeeper: accountKeeper}
}

// ClassifyProposal applies the same classifier to original proposal bytes.
// Callers must put those exact bytes in ctx.TxBytes before calling this method.
func (c StandardMsgSendClassifier) ClassifyProposal(
	ctx sdk.Context,
	tx sdk.Tx,
) (bool, error) {
	_, eligible, err := c.classify(ctx, tx, false)
	return eligible, err
}

// classify deliberately has a three-way result:
//
//   - eligible=false, err=nil: decode-valid ordinary upstream transaction;
//   - eligible=true, err=nil: bounded FixedSendGas transaction;
//   - err!=nil: deterministic FixedSendGas admission failure.
//
// Raw size is checked before decoded accessors. A decode-valid transaction over
// the ceiling is always ordinary, even if its canonical re-encoding is smaller.
func (c StandardMsgSendClassifier) classify(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
) (classification standardMsgSendClassification, eligible bool, err error) {
	defer func() {
		if recover() != nil {
			classification = standardMsgSendClassification{}
			eligible = false
			err = errorsmod.Wrap(
				sdkerrors.ErrTxDecode,
				"panic while classifying FixedSendGas transaction",
			)
		}
	}()

	if tx == nil {
		return standardMsgSendClassification{}, false, errorsmod.Wrap(
			sdkerrors.ErrTxDecode,
			"cannot classify a nil FixedSendGas transaction",
		)
	}
	if len(ctx.TxBytes()) > StandardMsgSendMaxTxBytes {
		return standardMsgSendClassification{}, false, nil
	}

	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return standardMsgSendClassification{}, false, nil
	}
	msg, ok := msgs[0].(*banktypes.MsgSend)
	if !ok || msg == nil || len(msg.Amount) != 1 {
		return standardMsgSendClassification{}, false, nil
	}
	if !msg.Amount.IsValid() || !msg.Amount.IsAllPositive() {
		return standardMsgSendClassification{}, false, errorsmod.Wrapf(
			sdkerrors.ErrInvalidCoins,
			"FixedSendGas requires one positive %s transfer amount",
			chainconfig.BaseDenom,
		)
	}
	if msg.Amount[0].Denom != chainconfig.BaseDenom {
		return standardMsgSendClassification{}, false, nil
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return standardMsgSendClassification{}, false, errorsmod.Wrap(
			sdkerrors.ErrTxDecode,
			"FixedSendGas MsgSend must implement sdk.FeeTx",
		)
	}
	declaredGas := feeTx.GetGas()
	fees := feeTx.GetFee()
	if !fees.IsValid() {
		return standardMsgSendClassification{}, false, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"invalid FixedSendGas fee: %s",
			fees,
		)
	}
	if len(feeTx.FeeGranter()) != 0 {
		return standardMsgSendClassification{}, false, nil
	}

	if simulate && declaredGas == 0 {
		if len(fees) > 1 || (len(fees) == 1 && fees[0].Denom != chainconfig.BaseDenom) {
			return standardMsgSendClassification{}, false, nil
		}
	} else if len(fees) != 1 || fees[0].Denom != chainconfig.BaseDenom {
		return standardMsgSendClassification{}, false, nil
	}

	if c.accountKeeper == nil {
		return standardMsgSendClassification{}, false, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas account keeper is not configured",
		)
	}
	memoTx, ok := tx.(sdk.TxWithMemo)
	if !ok {
		return standardMsgSendClassification{}, false, errorsmod.Wrap(
			sdkerrors.ErrTxDecode,
			"FixedSendGas transaction must implement sdk.TxWithMemo",
		)
	}
	memoLength := len(memoTx.GetMemo())
	if maxMemo := c.accountKeeper.GetParams(ctx).MaxMemoCharacters; uint64(memoLength) > maxMemo {
		return standardMsgSendClassification{}, false, errorsmod.Wrapf(
			sdkerrors.ErrMemoTooLarge,
			"maximum number of characters is %d but received %d characters",
			maxMemo,
			memoLength,
		)
	}

	from, err := c.accountKeeper.AddressCodec().StringToBytes(msg.FromAddress)
	if err != nil {
		return standardMsgSendClassification{}, false, errorsmod.Wrapf(
			sdkerrors.ErrInvalidAddress,
			"invalid FixedSendGas sender: %s",
			err,
		)
	}
	if _, err := c.accountKeeper.AddressCodec().StringToBytes(msg.ToAddress); err != nil {
		return standardMsgSendClassification{}, false, errorsmod.Wrapf(
			sdkerrors.ErrInvalidAddress,
			"invalid FixedSendGas recipient: %s",
			err,
		)
	}

	feePayer := sdk.AccAddress(feeTx.FeePayer())
	if !bytes.Equal(from, feePayer) {
		return standardMsgSendClassification{}, false, nil
	}
	singleKey, err := c.hasSingleSupportedSigner(ctx, tx, feePayer)
	if err != nil {
		return standardMsgSendClassification{}, false, err
	}
	if !singleKey {
		return standardMsgSendClassification{}, false, nil
	}

	extensionTx, ok := tx.(authante.HasExtensionOptionsTx)
	if !ok {
		return standardMsgSendClassification{}, false, errorsmod.Wrap(
			sdkerrors.ErrTxDecode,
			"FixedSendGas transaction must implement extension options",
		)
	}
	extensions := extensionTx.GetExtensionOptions()
	if len(extensions) > 1 {
		return standardMsgSendClassification{}, false, errorsmod.Wrap(
			sdkerrors.ErrUnknownExtensionOptions,
			"FixedSendGas permits at most one dynamic-fee extension",
		)
	}
	if len(extensions) == 1 {
		option := extensions[0]
		if option == nil || option.GetTypeUrl() != dynamicFeeExtensionURL {
			return standardMsgSendClassification{}, false, errorsmod.Wrap(
				sdkerrors.ErrUnknownExtensionOptions,
				"FixedSendGas has an unsupported critical extension",
			)
		}
		if _, ok := option.GetCachedValue().(*antetypes.ExtensionOptionDynamicFeeTx); !ok {
			return standardMsgSendClassification{}, false, errorsmod.Wrap(
				sdkerrors.ErrUnknownExtensionOptions,
				"FixedSendGas dynamic-fee extension is malformed",
			)
		}
	}
	if len(extensionTx.GetNonCriticalExtensionOptions()) != 0 {
		return standardMsgSendClassification{}, false, nil
	}

	if declaredGas < StandardMsgSendGas {
		if simulate {
			if declaredGas == 0 {
				// D=0 is the explicit gas-only estimation exception.
				return standardMsgSendClassification{
					feeTx:  feeTx,
					msg:    msg,
					sender: sdk.AccAddress(from),
				}, true, nil
			}

			// A non-zero simulation below G keeps ordinary SDK semantics.
			return standardMsgSendClassification{}, false, nil
		}
		return standardMsgSendClassification{}, false, errorsmod.Wrapf(
			sdkerrors.ErrInvalidGasLimit,
			"FixedSendGas requires at least %d declared gas",
			StandardMsgSendGas,
		)
	}

	return standardMsgSendClassification{
		feeTx:  feeTx,
		msg:    msg,
		sender: sdk.AccAddress(from),
	}, true, nil
}

func (c StandardMsgSendClassifier) hasSingleSupportedSigner(
	ctx context.Context,
	tx sdk.Tx,
	feePayer sdk.AccAddress,
) (bool, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return false, errorsmod.Wrap(
			sdkerrors.ErrTxDecode,
			"FixedSendGas transaction must implement signature verification",
		)
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return false, errorsmod.Wrap(sdkerrors.ErrTxDecode, "get FixedSendGas signers")
	}
	if len(signers) != 1 || !bytes.Equal(signers[0], feePayer) {
		return false, nil
	}
	signatures, err := sigTx.GetSignaturesV2()
	if err != nil {
		return false, errorsmod.Wrap(sdkerrors.ErrTxDecode, "get FixedSendGas signatures")
	}
	if len(signatures) != 1 {
		return false, nil
	}
	if _, ok := signatures[0].Data.(*signing.SingleSignatureData); !ok {
		return false, nil
	}
	pubKeys, err := sigTx.GetPubKeys()
	if err != nil {
		return false, errorsmod.Wrap(sdkerrors.ErrTxDecode, "get FixedSendGas public keys")
	}
	if len(pubKeys) != 1 {
		return false, nil
	}

	pubKey := pubKeys[0]
	if pubKey == nil {
		account := c.accountKeeper.GetAccount(ctx, feePayer)
		if account == nil {
			return false, nil
		}
		pubKey = account.GetPubKey()
	}

	return hasStandardMsgSendPubKey(pubKey), nil
}

func hasStandardMsgSendPubKey(pubKey cryptotypes.PubKey) bool {
	switch pubKey.(type) {
	case *secp256k1.PubKey, *ethsecp256k1.PubKey:
		return true
	default:
		return false
	}
}

// StandardMsgSendGasDecorator is the first setup router in the local Cosmos
// ante chain. Eligible FixedSendGas transactions get a staged accounting meter;
// ordinary Cosmos transactions retain the SDK SetUpContextDecorator.
type StandardMsgSendGasDecorator struct {
	classifier StandardMsgSendClassifier
	upstream   authante.SetUpContextDecorator
}

func NewStandardMsgSendGasDecorator(
	accountKeeper authante.AccountKeeper,
) StandardMsgSendGasDecorator {
	return StandardMsgSendGasDecorator{
		classifier: NewStandardMsgSendClassifier(accountKeeper),
		upstream:   authante.NewSetUpContextDecorator(),
	}
}

func (d StandardMsgSendGasDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (newCtx sdk.Context, err error) {
	classification, eligible := standardMsgSendClassificationFromContext(ctx)
	if !eligible {
		classified, classifiedEligible, classifyErr := d.classifier.classify(ctx, tx, simulate)
		if classifyErr != nil {
			return ctx, classifyErr
		}
		if !classifiedEligible {
			return d.upstream.AnteHandle(ctx, tx, simulate, next)
		}
		classification = &classified
	}

	actualMeter := storetypes.NewInfiniteGasMeter()
	if ctx.GasMeter() != nil {
		if consumed := ctx.GasMeter().GasConsumed(); consumed > 0 {
			actualMeter.ConsumeGas(consumed, "FixedSendGas pre-setup gas")
		}
	}
	meter := newStandardMsgSendGasMeter(actualMeter)
	fixedCtx := ctx.
		WithGasMeter(meter).
		WithValue(standardMsgSendContextKey{}, classification)
	if block := ctx.ConsensusParams().Block; block != nil &&
		block.MaxGas > 0 && StandardMsgSendGas > uint64(block.MaxGas) {
		return fixedCtx, errorsmod.Wrapf(
			sdkerrors.ErrInvalidGasLimit,
			"FixedSendGas accounting gas %d exceeds block max gas %d",
			StandardMsgSendGas,
			block.MaxGas,
		)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			switch outOfGas := recovered.(type) {
			case storetypes.ErrorOutOfGas:
				newCtx = fixedCtx
				err = errorsmod.Wrapf(
					sdkerrors.ErrOutOfGas,
					"out of gas in location: %v; gasWanted: %d; gasUsed: %d",
					outOfGas.Descriptor,
					StandardMsgSendGas,
					meter.GasConsumed(),
				)
			default:
				panic(recovered)
			}
		}
	}()

	newCtx, err = next(fixedCtx, tx, simulate)
	if newCtx.IsZero() {
		newCtx = fixedCtx
	}
	returnedMeter, ok := newCtx.GasMeter().(*standardMsgSendGasMeter)
	if !ok || returnedMeter != meter {
		if err != nil {
			// Preserve the canonical downstream error, but return the fixed
			// context so failed ante accounting cannot escape the staged meter.
			return fixedCtx, err
		}
		return fixedCtx, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"FixedSendGas meter was replaced during ante handling",
		)
	}
	if err != nil {
		return newCtx, err
	}
	meter.anteSucceeded = true
	return newCtx, nil
}

func standardMsgSendClassificationFromContext(
	ctx sdk.Context,
) (*standardMsgSendClassification, bool) {
	classification, ok := ctx.Value(standardMsgSendContextKey{}).(*standardMsgSendClassification)
	return classification, ok && classification != nil
}

func isStandardMsgSendGasContext(ctx sdk.Context) bool {
	_, classified := standardMsgSendClassificationFromContext(ctx)
	_, metered := ctx.GasMeter().(*standardMsgSendGasMeter)
	return classified && metered
}

type standardMsgSendGasMeter struct {
	actual        storetypes.GasMeter
	anteSucceeded bool
}

func newStandardMsgSendGasMeter(actual storetypes.GasMeter) *standardMsgSendGasMeter {
	return &standardMsgSendGasMeter{actual: actual}
}

func (m *standardMsgSendGasMeter) GasConsumed() storetypes.Gas {
	return storetypes.Gas(StandardMsgSendGas)
}

func (m *standardMsgSendGasMeter) GasConsumedToLimit() storetypes.Gas {
	if !m.anteSucceeded {
		return 0
	}
	return storetypes.Gas(StandardMsgSendGas)
}

func (m *standardMsgSendGasMeter) GasRemaining() storetypes.Gas {
	return 0
}

func (m *standardMsgSendGasMeter) Limit() storetypes.Gas {
	return storetypes.Gas(StandardMsgSendGas)
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
		"FixedSendGasMeter{accounting: %d, ante_succeeded: %t, actual: %s}",
		StandardMsgSendGas,
		m.anteSucceeded,
		m.actual.String(),
	)
}

func (m *standardMsgSendGasMeter) actualGasConsumed() storetypes.Gas {
	return m.actual.GasConsumed()
}
