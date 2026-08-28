package ante

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	coreaddress "cosmossdk.io/core/address"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	evmante "github.com/cosmos/evm/ante"
	evmanteevm "github.com/cosmos/evm/ante/evm"
	antetypes "github.com/cosmos/evm/ante/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func TestStandardMsgSendGasDecoratorEligibility(t *testing.T) {
	configureStandardMsgSendTestDenom()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	other := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	dynamicExtension, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{})
	require.NoError(t, err)

	tests := []struct {
		name             string
		simulate         bool
		mutate           func(*standardMsgSendTestTx)
		wantStandard     bool
		wantExecutionGas uint64
		wantErr          error
	}{
		{name: "exact gas", wantStandard: true},
		{
			name: "declared gas at multiplier threshold",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = 42_000
			},
			wantStandard:     true,
			wantExecutionGas: StandardMsgSendGas,
		},
		{
			name: "declared gas above standard",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = 200_000
			},
			wantStandard:     true,
			wantExecutionGas: 100_000,
		},
		{
			name: "dynamic fee extension",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.extensions = []*codectypes.Any{dynamicExtension}
			},
			wantStandard: true,
		},
		{
			name:     "simulation gas zero",
			simulate: true,
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = 0
			},
			wantStandard: true,
		},
		{
			name: "execution below standard",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = StandardMsgSendGas - 1
			},
			wantErr: sdkerrors.ErrInvalidGasLimit,
		},
		{
			name:     "simulation below standard falls back",
			simulate: true,
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = StandardMsgSendGas - 1
			},
		},
		{
			name: "memo above bound",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.memo = strings.Repeat("m", StandardMsgSendMaxMemoBytes+1)
			},
		},
		{
			name: "multiple send denoms",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).Amount = sdk.NewCoins(
					sdk.NewInt64Coin("adenom", 1),
					sdk.NewInt64Coin("bdenom", 1),
				)
			},
		},
		{
			name: "invalid recipient",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).ToAddress = "not-an-address"
			},
		},
		{
			name: "non-positive send amount",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).Amount = sdk.Coins{
					sdk.NewInt64Coin("send-denom", 0),
				}
			},
		},
		{
			name: "fee grant",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.granter = append(sdk.AccAddress(nil), payer...)
			},
		},
		{
			name: "fee payer differs from sender",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.payer = append(sdk.AccAddress(nil), other...)
				tx.signers = [][]byte{append([]byte(nil), other...)}
			},
		},
		{
			name: "multiple signers",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signers = append(tx.signers, append([]byte(nil), other...))
			},
		},
		{
			name: "unsupported public key",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.pubKeys = []cryptotypes.PubKey{ed25519.GenPrivKey().PubKey()}
			},
		},
		{
			name: "large single signature remains eligible",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signatures[0].Data = &signing.SingleSignatureData{
					Signature: bytes.Repeat([]byte{0x01}, 1_024),
				}
			},
			wantStandard: true,
		},
		{
			name: "multi signature data remains eligible",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signatures[0].Data = &signing.MultiSignatureData{}
			},
			wantStandard: true,
		},
	}

	decorator := NewStandardMsgSendGasDecorator(
		&standardMsgSendAccountKeeper{addressCodec: addressCodec},
		standardMsgSendTestMinGasMultiplier(),
		0,
	)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := newStandardMsgSendTestTx(t, addressCodec, payer)
			if test.mutate != nil {
				test.mutate(&tx)
			}

			originalMeter := storetypes.NewInfiniteGasMeter()
			ctx := sdk.Context{}.
				WithGasMeter(originalMeter).
				WithTxBytes(bytes.Repeat([]byte{0x01}, 4_096))
			nextCalled := false
			var nextTx sdk.Tx
			newCtx, err := decorator.AnteHandle(
				ctx,
				&tx,
				test.simulate,
				func(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
					nextCalled = true
					nextTx = tx
					return ctx, nil
				},
			)

			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				require.False(t, nextCalled)
				return
			}

			require.NoError(t, err)
			require.True(t, nextCalled)
			require.Same(t, &tx, nextTx)
			_, standardized := newCtx.GasMeter().(*standardMsgSendGasMeter)
			require.Equal(t, test.wantStandard, standardized)
			if test.wantStandard {
				wantLimit := tx.gas
				if wantLimit == 0 {
					wantLimit = StandardMsgSendGas
				}
				wantExecutionGas := test.wantExecutionGas
				if wantExecutionGas == 0 {
					wantExecutionGas = StandardMsgSendGas
				}
				require.Equal(t, wantExecutionGas, uint64(newCtx.GasMeter().GasConsumed()))
				require.Equal(t, wantLimit, uint64(newCtx.GasMeter().Limit()))
				require.Equal(
					t,
					wantLimit-wantExecutionGas,
					uint64(newCtx.GasMeter().GasRemaining()),
				)
				standardMeter := newCtx.GasMeter().(*standardMsgSendGasMeter)
				require.Equal(t, wantExecutionGas, uint64(standardMeter.executionGas))
			} else {
				require.Same(t, originalMeter, newCtx.GasMeter())
			}
		})
	}
}

func TestStandardMsgSendMalformedFeePayerFallsBackToCanonicalAnteValidation(t *testing.T) {
	configureStandardMsgSendTestDenom()
	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	tx := panickingFeePayerTestTx{standardMsgSendTestTx: newStandardMsgSendTestTx(
		t,
		addressCodec,
		payer,
	)}
	decorator := NewStandardMsgSendGasDecorator(
		&standardMsgSendAccountKeeper{addressCodec: addressCodec},
		standardMsgSendTestMinGasMultiplier(),
		0,
	)
	ctx := sdk.Context{}.WithGasMeter(storetypes.NewInfiniteGasMeter())
	wantErr := errors.New("downstream canonical validation error")
	nextCalls := 0

	newCtx, err := decorator.AnteHandle(
		ctx,
		tx,
		false,
		func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalls++
			return nextCtx, wantErr
		},
	)

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, 1, nextCalls)
	require.Same(t, ctx.GasMeter(), newCtx.GasMeter())
}

func TestStandardMsgSendGasByExecutionPhase(t *testing.T) {
	configureStandardMsgSendTestDenom()
	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	const declaredGas = uint64(200_000)

	tests := []struct {
		name         string
		context      sdk.Context
		simulate     bool
		maxCheckGas  uint64
		wantLimit    uint64
		wantConsumed uint64
	}{
		{
			name:         "check tx",
			context:      sdk.Context{}.WithIsCheckTx(true),
			wantLimit:    declaredGas,
			wantConsumed: 0,
		},
		{
			name:         "capped recheck tx",
			context:      sdk.Context{}.WithIsReCheckTx(true),
			maxCheckGas:  80_000,
			wantLimit:    80_000,
			wantConsumed: 0,
		},
		{
			name:         "simulate",
			context:      sdk.Context{}.WithExecMode(sdk.ExecModeSimulate),
			simulate:     true,
			wantLimit:    declaredGas,
			wantConsumed: 100_000,
		},
		{
			name:         "finalize",
			context:      sdk.Context{}.WithExecMode(sdk.ExecModeFinalize),
			wantLimit:    declaredGas,
			wantConsumed: 100_000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decorator := NewStandardMsgSendGasDecorator(
				&standardMsgSendAccountKeeper{addressCodec: addressCodec},
				standardMsgSendTestMinGasMultiplier(),
				test.maxCheckGas,
			)
			tx := newStandardMsgSendTestTx(t, addressCodec, payer)
			tx.gas = declaredGas
			ctx := test.context.WithGasMeter(storetypes.NewGasMeter(tx.gas))

			newCtx, err := decorator.AnteHandle(
				ctx,
				tx,
				test.simulate,
				func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
					nextCtx.GasMeter().ConsumeGas(tx.gas+1, "internal work above submitted limit")
					return nextCtx, nil
				},
			)

			require.NoError(t, err)
			require.Equal(t, declaredGas, tx.GetGas())
			require.Equal(t, test.wantLimit, uint64(newCtx.GasMeter().Limit()))
			require.Equal(t, test.wantConsumed, uint64(newCtx.GasMeter().GasConsumed()))
			standardMeter := newCtx.GasMeter().(*standardMsgSendGasMeter)
			require.Equal(t, uint64(100_000), uint64(standardMeter.executionGas))
			require.Equal(t, storetypes.GasConfig{}, newCtx.KVGasConfig())
			require.Equal(t, storetypes.GasConfig{}, newCtx.TransientKVGasConfig())
			require.False(t, newCtx.GasMeter().IsPastLimit())
			require.False(t, newCtx.GasMeter().IsOutOfGas())
		})
	}
}

func TestStandardMsgSendFeeAccounting(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)
	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x66}, 20))
	const (
		declaredGas  = uint64(200_000)
		executionGas = uint64(100_000)
		gasPrice     = int64(10)
	)

	t.Run("London admission uses declared gas and settlement uses execution gas", func(t *testing.T) {
		baseFee := sdkmath.LegacyNewDec(gasPrice)
		tipCap := sdkmath.LegacyNewDec(2).MulInt(evmtypes.DefaultPriorityReduction)
		feeCap := baseFee.Add(
			sdkmath.LegacyNewDec(3).MulInt(evmtypes.DefaultPriorityReduction),
		)
		extension, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{
			MaxPriorityPrice: tipCap,
		})
		require.NoError(t, err)

		tx := newStandardMsgSendTestTx(t, addressCodec, payer)
		tx.gas = declaredGas
		tx.fee = sdk.NewCoins(sdk.NewCoin(
			"agxn",
			feeCap.MulInt(sdkmath.NewIntFromUint64(declaredGas)).RoundInt(),
		))
		tx.extensions = []*codectypes.Any{extension}
		params := feemarkettypes.DefaultParams()
		params.BaseFee = baseFee
		checker := newCosmosDynamicFeeChecker(&params)
		effectivePrice := baseFee.Add(tipCap)

		checkFees, priority, err := checker(
			standardMsgSendFeeTestContext(1, declaredGas, executionGas, true),
			tx,
		)
		require.NoError(t, err)
		require.Equal(t, int64(2), priority)
		require.Equal(
			t,
			effectivePrice.MulInt(sdkmath.NewIntFromUint64(declaredGas)).Ceil().RoundInt(),
			checkFees.AmountOf("agxn"),
		)

		finalizeFees, _, err := checker(
			standardMsgSendFeeTestContext(1, declaredGas, executionGas, false),
			tx,
		)
		require.NoError(t, err)
		require.Equal(
			t,
			effectivePrice.MulInt(sdkmath.NewIntFromUint64(executionGas)).Ceil().RoundInt(),
			finalizeFees.AmountOf("agxn"),
		)
		require.Equal(t, declaredGas, tx.GetGas())
	})

	t.Run("fee covering only execution gas fails declared-gas admission", func(t *testing.T) {
		tx := newStandardMsgSendTestTx(t, addressCodec, payer)
		tx.gas = declaredGas
		tx.fee = sdk.NewCoins(sdk.NewInt64Coin("agxn", int64(executionGas)*gasPrice))
		params := feemarkettypes.DefaultParams()
		params.BaseFee = sdkmath.LegacyNewDec(gasPrice)

		_, _, err := newCosmosDynamicFeeChecker(&params)(
			standardMsgSendFeeTestContext(1, declaredGas, executionGas, true),
			tx,
		)
		require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
	})

	t.Run("standard dispatch does not apply upstream raw fractional base fee", func(t *testing.T) {
		feeCap := sdkmath.LegacyMustNewDecFromStr("10.5")
		tx := newStandardMsgSendTestTx(t, addressCodec, payer)
		tx.gas = declaredGas
		tx.fee = sdk.NewCoins(sdk.NewCoin(
			"agxn",
			feeCap.MulInt(sdkmath.NewIntFromUint64(declaredGas)).RoundInt(),
		))
		params := feemarkettypes.DefaultParams()
		params.BaseFee = sdkmath.LegacyMustNewDecFromStr("10.9")

		fees, _, err := newCosmosDynamicFeeChecker(&params)(
			standardMsgSendFeeTestContext(1, declaredGas, executionGas, true),
			tx,
		)
		require.NoError(t, err)
		require.Equal(t, tx.GetFee(), fees)
	})

	t.Run("validator minimum price reserves declared fee then settles execution fee", func(t *testing.T) {
		tx := newStandardMsgSendTestTx(t, addressCodec, payer)
		tx.gas = declaredGas
		tx.fee = sdk.NewCoins(sdk.NewInt64Coin("agxn", int64(declaredGas)*gasPrice))
		checker := newStandardMsgSendValidatorFeeChecker()
		checkCtx := standardMsgSendFeeTestContext(1, declaredGas, executionGas, true).
			WithMinGasPrices(sdk.DecCoins{
				sdk.NewDecCoinFromDec("agxn", sdkmath.LegacyNewDec(gasPrice)),
			})

		checkFees, priority, err := checker(checkCtx, tx)
		require.NoError(t, err)
		require.Equal(t, tx.GetFee(), checkFees)
		require.Equal(t, gasPrice, priority)

		finalizeFees, _, err := checker(
			standardMsgSendFeeTestContext(1, declaredGas, executionGas, false),
			tx,
		)
		require.NoError(t, err)
		require.Equal(
			t,
			sdk.NewCoins(sdk.NewInt64Coin("agxn", int64(executionGas)*gasPrice)),
			finalizeFees,
		)
	})

	t.Run("ordinary transactions retain the upstream checker", func(t *testing.T) {
		tx := newStandardMsgSendTestTx(t, addressCodec, payer)
		tx.gas = declaredGas
		tx.fee = sdk.NewCoins(sdk.NewInt64Coin("agxn", int64(declaredGas)*gasPrice))
		params := feemarkettypes.DefaultParams()
		params.BaseFee = sdkmath.LegacyNewDec(gasPrice)
		ctx := sdk.Context{}.
			WithBlockHeight(1).
			WithGasMeter(storetypes.NewInfiniteGasMeter())

		wantFees, wantPriority, err := evmanteevm.NewDynamicFeeChecker(&params)(ctx, tx)
		require.NoError(t, err)
		gotFees, gotPriority, err := newCosmosDynamicFeeChecker(&params)(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, wantFees, gotFees)
		require.Equal(t, wantPriority, gotPriority)
	})
}

func TestStandardMsgSendEthereumFeePolicyEdges(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)
	ctx := standardMsgSendFeeTestContext(1, 42_000, 21_000, true)
	params := feemarkettypes.DefaultParams()
	params.BaseFee = sdkmath.LegacyMustNewDecFromStr("10.9")
	require.Equal(t, sdkmath.LegacyNewDec(10), standardMsgSendEthereumBaseFee(ctx, &params))
	params.NoBaseFee = true
	require.True(t, standardMsgSendEthereumBaseFee(ctx, &params).IsZero())

	params = feemarkettypes.DefaultParams()
	params.BaseFee = sdkmath.LegacyNewDec(10)
	params.MinGasPrice = sdkmath.LegacyNewDec(15)
	extension, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{
		MaxPriorityPrice: sdkmath.LegacyNewDec(1),
	})
	require.NoError(t, err)
	addressCodec := evmaddress.NewEvmCodec("guru")
	tx := newStandardMsgSendTestTx(
		t,
		addressCodec,
		sdk.AccAddress(bytes.Repeat([]byte{0x77}, 20)),
	)
	tx.gas = 42_000
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin("agxn", int64(tx.gas)*20))
	tx.extensions = []*codectypes.Any{extension}

	_, err = newStandardMsgSendMinGasPriceDecorator(&params).AnteHandle(
		ctx,
		tx,
		false,
		func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return nextCtx, nil
		},
	)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
}

func TestAnteRouterPreservesUpstreamErrors(t *testing.T) {
	options := evmante.HandlerOptions{}
	localHandler := NewAnteHandler(options)
	upstreamHandler := evmante.NewAnteHandler(options)
	tests := []struct {
		name string
		tx   sdk.Tx
	}{
		{name: "nil transaction"},
		{
			name: "unknown extension",
			tx: anteRouterTestTx{extensions: []*codectypes.Any{{
				TypeUrl: "/test.unsupported.ExtensionOption",
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := sdk.Context{}
			_, wantErr := upstreamHandler(ctx, test.tx, false)
			_, gotErr := localHandler(ctx, test.tx, false)
			require.Error(t, wantErr)
			require.EqualError(t, gotErr, wantErr.Error())
		})
	}
}

func TestOrdinaryDynamicFeeEffectivePriceFloor(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	const gas = uint64(100)
	tests := []struct {
		name           string
		baseFee        string
		baseFeeNil     bool
		minGasPrice    string
		feeCap         string
		tipCap         *string
		effectivePrice string
		wantErr        bool
	}{
		{
			name:           "below floor",
			baseFee:        "10",
			minGasPrice:    "15",
			feeCap:         "20",
			tipCap:         decString("1"),
			effectivePrice: "11",
			wantErr:        true,
		},
		{
			name:           "exact floor",
			baseFee:        "10",
			minGasPrice:    "15",
			feeCap:         "20",
			tipCap:         decString("5"),
			effectivePrice: "15",
		},
		{
			name:           "above floor",
			baseFee:        "10",
			minGasPrice:    "15",
			feeCap:         "20",
			tipCap:         decString("6"),
			effectivePrice: "16",
		},
		{
			name:           "nil tip",
			baseFee:        "10",
			minGasPrice:    "15",
			feeCap:         "20",
			effectivePrice: "10",
			wantErr:        true,
		},
		{
			name:           "zero tip",
			baseFee:        "10",
			minGasPrice:    "15",
			feeCap:         "20",
			tipCap:         decString("0"),
			effectivePrice: "10",
			wantErr:        true,
		},
		{
			name:           "zero base fee",
			baseFee:        "0",
			minGasPrice:    "15",
			feeCap:         "20",
			tipCap:         decString("14"),
			effectivePrice: "14",
			wantErr:        true,
		},
		{
			name:           "nil base fee",
			baseFee:        "0",
			baseFeeNil:     true,
			minGasPrice:    "15",
			feeCap:         "20",
			tipCap:         decString("14"),
			effectivePrice: "14",
			wantErr:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := dynamicFeeFloorParams(test.baseFee, test.minGasPrice)
			if test.baseFeeNil {
				params.BaseFee = sdkmath.LegacyDec{}
			}
			tx := newOrdinaryDynamicFeeTestTx(t, gas, test.feeCap, test.tipCap)
			ctx := ordinaryDynamicFeeTestContext(1)

			feeCap := sdkmath.LegacyNewDecFromInt(tx.GetFee().AmountOf(evmtypes.GetEVMCoinDenom())).
				QuoInt(sdkmath.NewIntFromUint64(gas))
			requiredTotal := params.MinGasPrice.MulInt64(int64(gas)).Ceil().RoundInt()
			require.True(t, feeCap.GTE(params.MinGasPrice), "upfront fee cap must satisfy the global floor")
			require.True(t, tx.GetFee().AmountOf(evmtypes.GetEVMCoinDenom()).GTE(requiredTotal))

			wantFees, wantPriority, upstreamErr := evmanteevm.NewDynamicFeeChecker(&params)(ctx, tx)
			require.NoError(t, upstreamErr, "the upstream checker must accept the regression input")
			effectivePrice := sdkmath.LegacyMustNewDecFromStr(test.effectivePrice)
			require.Equal(
				t,
				effectivePrice.MulInt64(int64(gas)).Ceil().RoundInt(),
				wantFees.AmountOf(evmtypes.GetEVMCoinDenom()),
			)

			gotFees, gotPriority, err := newCosmosDynamicFeeChecker(&params)(ctx, tx)
			if test.wantErr {
				require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
				require.Nil(t, gotFees)
				require.Zero(t, gotPriority)
				return
			}

			require.NoError(t, err)
			require.Equal(t, wantFees, gotFees)
			require.Equal(t, wantPriority, gotPriority)
		})
	}
}

func TestOrdinaryDynamicFeeFloorUsesDecimalPriceNotRoundedTotal(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	const gas = uint64(10)
	params := dynamicFeeFloorParams("1.08", "1.1")
	tx := newOrdinaryDynamicFeeTestTx(t, gas, "1.1", decString("0.01"))
	ctx := ordinaryDynamicFeeTestContext(1)

	providedRoundedTotal := sdkmath.LegacyMustNewDecFromStr("1.09").
		MulInt64(int64(gas)).
		Ceil().
		RoundInt()
	requiredRoundedTotal := params.MinGasPrice.
		MulInt64(int64(gas)).
		Ceil().
		RoundInt()
	require.Equal(t, requiredRoundedTotal, providedRoundedTotal, "rounded totals are intentionally equal")
	require.Equal(t, sdkmath.NewInt(11), tx.GetFee().AmountOf(evmtypes.GetEVMCoinDenom()))

	_, _, upstreamErr := evmanteevm.NewDynamicFeeChecker(&params)(ctx, tx)
	require.NoError(t, upstreamErr)
	_, _, err := newCosmosDynamicFeeChecker(&params)(ctx, tx)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
}

func TestOrdinaryDynamicFeeFloorPreservesUpstreamSemantics(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	t.Run("minimum gas price zero", func(t *testing.T) {
		params := dynamicFeeFloorParams("10", "0")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("dynamic extension absent", func(t *testing.T) {
		params := dynamicFeeFloorParams("10", "15")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0"))
		tx.extensions = nil
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("genesis fallback", func(t *testing.T) {
		params := dynamicFeeFloorParams("10", "15")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(0), &params, tx)
	})

	t.Run("negative tip error", func(t *testing.T) {
		params := dynamicFeeFloorParams("10", "15")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("-1"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("zero gas error", func(t *testing.T) {
		params := dynamicFeeFloorParams("10", "15")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("1"))
		tx.gas = 0
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("fee cap below base fee error", func(t *testing.T) {
		params := dynamicFeeFloorParams("10", "15")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "9", decString("1"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("tip above fee cap remains capped", func(t *testing.T) {
		params := dynamicFeeFloorParams("10", "15")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("25"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("raw fractional base fee is preserved when no base fee flag is set", func(t *testing.T) {
		params := dynamicFeeFloorParams("10.9", "11")
		params.NoBaseFee = true
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0.1"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("future base fee enable height still uses raw base fee", func(t *testing.T) {
		params := dynamicFeeFloorParams("10.9", "11")
		params.EnableHeight = 100
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0.1"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})

	t.Run("pre London fallback", func(t *testing.T) {
		configureStandardMsgSendFeeTestChain(t, 100)
		t.Cleanup(func() { configureStandardMsgSendFeeTestChain(t, 0) })
		params := dynamicFeeFloorParams("10", "15")
		tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("-1"))
		assertOrdinaryDynamicCheckerMatchesUpstream(t, ordinaryDynamicFeeTestContext(1), &params, tx)
	})
}

func TestOrdinaryDynamicFeeFloorRejectsBeforeDeductionAndDownstream(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	params := dynamicFeeFloorParams("10", "15")
	tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0"))
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x81}, 20))
	tx.payer = payer
	accountKeeper := &dynamicFeeFloorAccountKeeper{
		addressCodec:  evmaddress.NewEvmCodec("guru"),
		account:       authtypes.NewBaseAccountWithAddress(payer),
		moduleAddress: sdk.AccAddress(bytes.Repeat([]byte{0x82}, 20)),
	}
	bankKeeper := &dynamicFeeFloorBankKeeper{}
	decorator := authante.NewDeductFeeDecorator(
		accountKeeper,
		bankKeeper,
		nil,
		newCosmosDynamicFeeChecker(&params),
	)
	ctx := ordinaryDynamicFeeTestContext(1).WithEventManager(sdk.NewEventManager())
	nextCalls := 0

	_, err := decorator.AnteHandle(ctx, tx, false, func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		nextCalls++
		return nextCtx, nil
	})

	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
	require.Zero(t, bankKeeper.sendCalls)
	require.Zero(t, nextCalls)
}

func TestOrdinaryDynamicFeeFloorPreservesSimulation(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	params := dynamicFeeFloorParams("10", "15")
	tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0"))
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x91}, 20))
	tx.payer = payer
	accountKeeper := &dynamicFeeFloorAccountKeeper{
		addressCodec:  evmaddress.NewEvmCodec("guru"),
		account:       authtypes.NewBaseAccountWithAddress(payer),
		moduleAddress: sdk.AccAddress(bytes.Repeat([]byte{0x92}, 20)),
	}
	bankKeeper := &dynamicFeeFloorBankKeeper{}
	decorator := authante.NewDeductFeeDecorator(
		accountKeeper,
		bankKeeper,
		nil,
		newCosmosDynamicFeeChecker(&params),
	)
	ctx := ordinaryDynamicFeeTestContext(1).WithEventManager(sdk.NewEventManager())
	nextCalls := 0

	_, err := decorator.AnteHandle(ctx, tx, true, func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		nextCalls++
		return nextCtx, nil
	})

	require.NoError(t, err)
	require.Equal(t, 1, bankKeeper.sendCalls)
	require.Equal(t, 1, nextCalls)
}

func TestOrdinaryDynamicFeeFloorCoversStandardMsgSendFallbacks(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	params := dynamicFeeFloorParams("10", "15")
	addressCodec := evmaddress.NewEvmCodec("guru")
	decorator := NewStandardMsgSendGasDecorator(
		&standardMsgSendAccountKeeper{addressCodec: addressCodec},
		standardMsgSendTestMinGasMultiplier(),
		0,
	)
	tests := []struct {
		name   string
		mutate func(*standardMsgSendTestTx)
	}{
		{
			name: "multiple messages",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs = append(tx.msgs, tx.msgs[0])
			},
		},
		{
			name: "fee grant",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.granter = append(sdk.AccAddress(nil), tx.payer...)
			},
		},
		{
			name: "classifier fallback MsgSend",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.memo = strings.Repeat("m", StandardMsgSendMaxMemoBytes+1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0"))
			test.mutate(&tx)
			checkerCalls := 0

			_, err := decorator.AnteHandle(
				ordinaryDynamicFeeTestContext(1),
				tx,
				false,
				func(nextCtx sdk.Context, nextTx sdk.Tx, _ bool) (sdk.Context, error) {
					checkerCalls++
					require.False(t, isStandardMsgSendGasContext(nextCtx))
					_, _, checkErr := newCosmosDynamicFeeChecker(&params)(nextCtx, nextTx)
					return nextCtx, checkErr
				},
			)

			require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
			require.Equal(t, 1, checkerCalls)
		})
	}
}

func TestOrdinaryDynamicFeeFloorUsesUpdatedMinGasPrice(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	params := dynamicFeeFloorParams("10", "10")
	tx := newOrdinaryDynamicFeeTestTx(t, 100, "20", decString("0"))
	checker := newCosmosDynamicFeeChecker(&params)
	ctx := ordinaryDynamicFeeTestContext(1)

	_, _, err := checker(ctx, tx)
	require.NoError(t, err)

	params.MinGasPrice = sdkmath.LegacyMustNewDecFromStr("10.000000000000000001")
	_, _, err = checker(ctx, tx)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
}

func dynamicFeeFloorParams(baseFee string, minGasPrice string) feemarkettypes.Params {
	params := feemarkettypes.DefaultParams()
	params.BaseFee = sdkmath.LegacyMustNewDecFromStr(baseFee)
	params.MinGasPrice = sdkmath.LegacyMustNewDecFromStr(minGasPrice)
	return params
}

func newOrdinaryDynamicFeeTestTx(
	t *testing.T,
	gas uint64,
	feeCap string,
	tipCap *string,
) standardMsgSendTestTx {
	t.Helper()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x80}, 20))
	tx := newStandardMsgSendTestTx(t, addressCodec, payer)
	tx.gas = gas
	feeCapDec := sdkmath.LegacyMustNewDecFromStr(feeCap)
	feeAmount := feeCapDec.MulInt(sdkmath.NewIntFromUint64(gas)).Ceil().RoundInt()
	tx.fee = sdk.NewCoins(sdk.NewCoin(evmtypes.GetEVMCoinDenom(), feeAmount))

	var tip sdkmath.LegacyDec
	if tipCap != nil {
		tip = sdkmath.LegacyMustNewDecFromStr(*tipCap)
	}
	extension, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{
		MaxPriorityPrice: tip,
	})
	require.NoError(t, err)
	tx.extensions = []*codectypes.Any{extension}
	return tx
}

func ordinaryDynamicFeeTestContext(height int64) sdk.Context {
	return sdk.Context{}.
		WithBlockHeight(height).
		WithGasMeter(storetypes.NewInfiniteGasMeter())
}

func assertOrdinaryDynamicCheckerMatchesUpstream(
	t *testing.T,
	ctx sdk.Context,
	params *feemarkettypes.Params,
	tx standardMsgSendTestTx,
) {
	t.Helper()

	wantFees, wantPriority, wantErr := evmanteevm.NewDynamicFeeChecker(params)(ctx, tx)
	gotFees, gotPriority, gotErr := newCosmosDynamicFeeChecker(params)(ctx, tx)
	if wantErr != nil {
		require.EqualError(t, gotErr, wantErr.Error())
		require.Equal(t, wantFees, gotFees)
		require.Equal(t, wantPriority, gotPriority)
		return
	}

	require.NoError(t, gotErr)
	require.Equal(t, wantFees, gotFees)
	require.Equal(t, wantPriority, gotPriority)
}

func decString(value string) *string {
	return &value
}

type dynamicFeeFloorAccountKeeper struct {
	authante.AccountKeeper
	addressCodec  coreaddress.Codec
	account       sdk.AccountI
	moduleAddress sdk.AccAddress
}

func (keeper *dynamicFeeFloorAccountKeeper) AddressCodec() coreaddress.Codec {
	return keeper.addressCodec
}

func (keeper *dynamicFeeFloorAccountKeeper) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI {
	return keeper.account
}

func (keeper *dynamicFeeFloorAccountKeeper) GetModuleAddress(string) sdk.AccAddress {
	return keeper.moduleAddress
}

type dynamicFeeFloorBankKeeper struct {
	authtypes.BankKeeper
	sendCalls int
}

func (keeper *dynamicFeeFloorBankKeeper) SendCoinsFromAccountToModule(
	context.Context,
	sdk.AccAddress,
	string,
	sdk.Coins,
) error {
	keeper.sendCalls++
	return nil
}

func configureStandardMsgSendFeeTestChain(t *testing.T, londonHeight int64) {
	t.Helper()
	configureStandardMsgSendTestDenom()
	chainConfig := evmtypes.DefaultChainConfig(0)
	if londonHeight > 0 {
		height := sdkmath.NewInt(londonHeight)
		chainConfig.LondonBlock = &height
		chainConfig.ArrowGlacierBlock = &height
		chainConfig.GrayGlacierBlock = &height
		chainConfig.MergeNetsplitBlock = &height
	}
	require.NoError(t, evmtypes.SetChainConfig(chainConfig))
}

func standardMsgSendFeeTestContext(
	height int64,
	declaredGas uint64,
	executionGas uint64,
	checkTx bool,
) sdk.Context {
	return sdk.Context{}.
		WithBlockHeight(height).
		WithIsCheckTx(checkTx).
		WithGasMeter(&standardMsgSendGasMeter{
			actual:       storetypes.NewInfiniteGasMeter(),
			limit:        storetypes.Gas(declaredGas),
			reportedGas:  storetypes.Gas(executionGas),
			executionGas: storetypes.Gas(executionGas),
		})
}

func standardMsgSendTestMinGasMultiplier() sdkmath.LegacyDec {
	return sdkmath.LegacyNewDecWithPrec(5, 1)
}

type standardMsgSendAccountKeeper struct {
	authante.AccountKeeper
	addressCodec coreaddress.Codec
	account      sdk.AccountI
}

func (keeper *standardMsgSendAccountKeeper) AddressCodec() coreaddress.Codec {
	return keeper.addressCodec
}

func (keeper *standardMsgSendAccountKeeper) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI {
	return keeper.account
}

type standardMsgSendTestTx struct {
	msgs                  []sdk.Msg
	fee                   sdk.Coins
	gas                   uint64
	payer                 sdk.AccAddress
	granter               sdk.AccAddress
	memo                  string
	unordered             bool
	extensions            []*codectypes.Any
	nonCriticalExtensions []*codectypes.Any
	signers               [][]byte
	pubKeys               []cryptotypes.PubKey
	signatures            []signing.SignatureV2
}

type panickingFeePayerTestTx struct {
	standardMsgSendTestTx
}

func (panickingFeePayerTestTx) FeePayer() []byte {
	panic("malformed fee payer")
}

type anteRouterTestTx struct {
	extensions []*codectypes.Any
}

func (anteRouterTestTx) GetMsgs() []sdk.Msg { return nil }

func (anteRouterTestTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx anteRouterTestTx) GetExtensionOptions() []*codectypes.Any { return tx.extensions }

func (anteRouterTestTx) GetNonCriticalExtensionOptions() []*codectypes.Any { return nil }

var (
	_ sdk.FeeTx                      = standardMsgSendTestTx{}
	_ sdk.TxWithMemo                 = standardMsgSendTestTx{}
	_ sdk.TxWithUnordered            = standardMsgSendTestTx{}
	_ authante.HasExtensionOptionsTx = standardMsgSendTestTx{}
	_ authante.HasExtensionOptionsTx = anteRouterTestTx{}
	_ authsigning.SigVerifiableTx    = standardMsgSendTestTx{}
)

func (tx standardMsgSendTestTx) GetMsgs() []sdk.Msg { return tx.msgs }

func (standardMsgSendTestTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx standardMsgSendTestTx) GetGas() uint64 { return tx.gas }

func (tx standardMsgSendTestTx) GetFee() sdk.Coins { return tx.fee }

func (tx standardMsgSendTestTx) FeePayer() []byte { return tx.payer }

func (tx standardMsgSendTestTx) FeeGranter() []byte { return tx.granter }

func (tx standardMsgSendTestTx) GetMemo() string { return tx.memo }

func (tx standardMsgSendTestTx) GetUnordered() bool { return tx.unordered }

func (standardMsgSendTestTx) GetTimeoutTimeStamp() time.Time { return time.Time{} }

func (tx standardMsgSendTestTx) GetExtensionOptions() []*codectypes.Any { return tx.extensions }

func (tx standardMsgSendTestTx) GetNonCriticalExtensionOptions() []*codectypes.Any {
	return tx.nonCriticalExtensions
}

func (tx standardMsgSendTestTx) GetSigners() ([][]byte, error) { return tx.signers, nil }

func (tx standardMsgSendTestTx) GetPubKeys() ([]cryptotypes.PubKey, error) {
	return tx.pubKeys, nil
}

func (tx standardMsgSendTestTx) GetSignaturesV2() ([]signing.SignatureV2, error) {
	return tx.signatures, nil
}

func newStandardMsgSendTestTx(
	t *testing.T,
	addressCodec coreaddress.Codec,
	payer sdk.AccAddress,
) standardMsgSendTestTx {
	t.Helper()

	from, err := addressCodec.BytesToString(payer)
	require.NoError(t, err)
	to, err := addressCodec.BytesToString(sdk.AccAddress(bytes.Repeat([]byte{0x77}, 20)))
	require.NoError(t, err)
	pubKey := secp256k1.GenPrivKeyFromSecret([]byte("standard-msgsend-test-key")).PubKey()

	return standardMsgSendTestTx{
		msgs: []sdk.Msg{&banktypes.MsgSend{
			FromAddress: from,
			ToAddress:   to,
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("send-denom", 1)),
		}},
		fee:        sdk.NewCoins(sdk.NewInt64Coin(evmtypes.GetEVMCoinDenom(), 1)),
		gas:        StandardMsgSendGas,
		payer:      append(sdk.AccAddress(nil), payer...),
		signers:    [][]byte{append([]byte(nil), payer...)},
		pubKeys:    []cryptotypes.PubKey{pubKey},
		signatures: []signing.SignatureV2{{PubKey: pubKey}},
	}
}

func configureStandardMsgSendTestDenom() {
	evmtypes.SetDefaultEvmCoinInfo(evmtypes.EvmCoinInfo{
		Denom:         "agxn",
		ExtendedDenom: "agxn",
		DisplayDenom:  "gxn",
		Decimals:      18,
	})
}
