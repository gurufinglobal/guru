package ante

import (
	"bytes"
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	antetypes "github.com/cosmos/evm/ante/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	// StandardMsgSendGas is the intrinsic execution-gas floor for the bounded
	// MsgSend class.
	StandardMsgSendGas uint64 = 21_000

	// StandardMsgSendMaxMemoBytes keeps the standard-send class bounded even if the
	// chain's auth parameter is raised later.
	StandardMsgSendMaxMemoBytes = 256
)

// StandardMsgSendGasDecorator assigns Ethereum-compatible minimum execution
// gas, with a 21k floor, to a deliberately narrow MsgSend shape. The submitted
// gas limit remains visible as the public meter limit, while internal ante and
// message work is tracked by an unbounded meter. Transactions outside this
// class continue through the ordinary Cosmos gas path.
type StandardMsgSendGasDecorator struct {
	accountKeeper    authante.AccountKeeper
	minGasMultiplier sdkmath.LegacyDec
	maxTxGasWanted   uint64
}

func NewStandardMsgSendGasDecorator(
	accountKeeper authante.AccountKeeper,
	minGasMultiplier sdkmath.LegacyDec,
	maxTxGasWanted uint64,
) StandardMsgSendGasDecorator {
	return StandardMsgSendGasDecorator{
		accountKeeper:    accountKeeper,
		minGasMultiplier: minGasMultiplier,
		maxTxGasWanted:   maxTxGasWanted,
	}
}

func (d StandardMsgSendGasDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	eligible, publicGasLimit, insufficientGas, dependenciesMissing := d.classify(ctx, tx, simulate)
	if insufficientGas {
		return ctx, errorsmod.Wrapf(
			sdkerrors.ErrInvalidGasLimit,
			"standard MsgSend requires at least %d gas",
			StandardMsgSendGas,
		)
	}
	if dependenciesMissing {
		return ctx, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"standard MsgSend gas dependencies are not configured",
		)
	}
	if !eligible {
		return next(ctx, tx, simulate)
	}

	executionGas, ok := standardMsgSendExecutionGas(publicGasLimit, d.minGasMultiplier)
	if !ok {
		return ctx, errorsmod.Wrap(
			sdkerrors.ErrLogic,
			"standard MsgSend minimum gas multiplier is invalid",
		)
	}

	actualMeter := storetypes.NewInfiniteGasMeter()
	if consumed := ctx.GasMeter().GasConsumed(); consumed > 0 {
		actualMeter.ConsumeGas(consumed, "standard MsgSend pre-classification gas")
	}
	gasWanted := publicGasLimit
	if (ctx.IsCheckTx() || ctx.IsReCheckTx()) &&
		d.maxTxGasWanted > 0 && gasWanted > d.maxTxGasWanted {
		gasWanted = d.maxTxGasWanted
	}

	return next(
		ctx.
			WithKVGasConfig(storetypes.GasConfig{}).
			WithTransientKVGasConfig(storetypes.GasConfig{}).
			WithGasMeter(newStandardMsgSendGasMeter(
				actualMeter,
				gasWanted,
				executionGas,
				simulate || (!ctx.IsCheckTx() && !ctx.IsReCheckTx()),
			)),
		tx,
		simulate,
	)
}

// standardMsgSendExecutionGas mirrors Cosmos EVM v0.6.1 minimum-gas
// settlement and preserves the 21k intrinsic floor for a standard send.
func standardMsgSendExecutionGas(
	declaredGas uint64,
	minGasMultiplier sdkmath.LegacyDec,
) (uint64, bool) {
	if declaredGas == 0 {
		return StandardMsgSendGas, true
	}
	if minGasMultiplier.IsNil() {
		minGasMultiplier = sdkmath.LegacyZeroDec()
	}
	if minGasMultiplier.IsNegative() || minGasMultiplier.GT(sdkmath.LegacyOneDec()) {
		return 0, false
	}

	minimumGasUsed := sdkmath.LegacyNewDecFromInt(sdkmath.NewIntFromUint64(declaredGas)).
		Mul(minGasMultiplier).
		TruncateInt()
	if !minimumGasUsed.IsUint64() {
		return 0, false
	}

	executionGas := minimumGasUsed.Uint64()
	if executionGas < StandardMsgSendGas {
		executionGas = StandardMsgSendGas
	}

	return executionGas, true
}

// classify contains only standard-send classification. The recovery boundary is
// intentionally narrower than the downstream ante call: malformed fee payer,
// granter, signer, or message accessors fall back to the ordinary path, where
// the normal decorators determine the canonical error without replaying work.
func (d StandardMsgSendGasDecorator) classify(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
) (eligible bool, publicGasLimit uint64, insufficientGas bool, dependenciesMissing bool) {
	defer func() {
		if recover() != nil {
			eligible = false
			publicGasLimit = 0
			insufficientGas = false
			dependenciesMissing = false
		}
	}()

	feeTx, msg, candidate := standardMsgSendShapeWithoutGasLimit(tx)
	if !candidate {
		return false, 0, false, false
	}

	declaredGas := feeTx.GetGas()
	if simulate {
		switch {
		case declaredGas == 0:
			publicGasLimit = StandardMsgSendGas
		case declaredGas < StandardMsgSendGas:
			return false, 0, false, false
		default:
			publicGasLimit = declaredGas
		}
	} else if declaredGas < StandardMsgSendGas {
		return false, 0, true, false
	} else {
		publicGasLimit = declaredGas
	}

	if d.accountKeeper == nil {
		return false, 0, false, true
	}

	feePayer := feeTx.FeePayer()
	from, err := d.accountKeeper.AddressCodec().StringToBytes(msg.FromAddress)
	if err != nil || !bytes.Equal(from, feePayer) {
		return false, 0, false, false
	}
	if _, err = d.accountKeeper.AddressCodec().StringToBytes(msg.ToAddress); err != nil ||
		!d.hasSingleSupportedSigner(ctx, tx, feePayer) {
		return false, 0, false, false
	}

	return true, publicGasLimit, false, false
}

func standardMsgSendShapeWithoutGasLimit(tx sdk.Tx) (sdk.FeeTx, *banktypes.MsgSend, bool) {
	if tx == nil {
		return nil, nil, false
	}

	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil, nil, false
	}
	msg, ok := msgs[0].(*banktypes.MsgSend)
	if !ok || msg == nil || len(msg.Amount) != 1 ||
		!msg.Amount.IsValid() || !msg.Amount.IsAllPositive() {
		return nil, nil, false
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok || !feeTx.GetFee().IsValid() ||
		len(feeTx.FeeGranter()) != 0 || len(feeTx.GetFee()) > 1 {
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

func (d StandardMsgSendGasDecorator) hasSingleSupportedSigner(
	ctx context.Context,
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

	return hasStandardMsgSendPubKey(pubKey)
}

func hasStandardMsgSendPubKey(pubKey cryptotypes.PubKey) bool {
	switch pubKey.(type) {
	case *secp256k1.PubKey, *ethsecp256k1.PubKey:
		return true
	default:
		return false
	}
}

type standardMsgSendGasMeter struct {
	actual       storetypes.GasMeter
	limit        storetypes.Gas
	reportedGas  storetypes.Gas
	executionGas storetypes.Gas
}

func newStandardMsgSendGasMeter(
	actual storetypes.GasMeter,
	limit uint64,
	executionGas uint64,
	reportExecutionGas bool,
) storetypes.GasMeter {
	reportedGas := uint64(0)
	if reportExecutionGas {
		reportedGas = executionGas
	}

	return &standardMsgSendGasMeter{
		actual:       actual,
		limit:        storetypes.Gas(limit),
		reportedGas:  storetypes.Gas(reportedGas),
		executionGas: storetypes.Gas(executionGas),
	}
}

func (m *standardMsgSendGasMeter) GasConsumed() storetypes.Gas {
	return m.reportedGas
}

func (m *standardMsgSendGasMeter) GasConsumedToLimit() storetypes.Gas {
	return m.reportedGas
}

func (m *standardMsgSendGasMeter) GasRemaining() storetypes.Gas {
	if m.reportedGas >= m.limit {
		return 0
	}

	return m.limit - m.reportedGas
}

func (m *standardMsgSendGasMeter) Limit() storetypes.Gas {
	return m.limit
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
		"StandardMsgSendGasMeter{limit: %d, execution: %d, reported: %d, actual: %s}",
		m.limit,
		m.executionGas,
		m.reportedGas,
		m.actual.String(),
	)
}

func (m *standardMsgSendGasMeter) actualGasConsumed() storetypes.Gas {
	return m.actual.GasConsumed()
}
