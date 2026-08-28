package ante

import (
	"bytes"
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	coreaddress "cosmossdk.io/core/address"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	evmante "github.com/cosmos/evm/ante"
	evmanteevm "github.com/cosmos/evm/ante/evm"
	antetypes "github.com/cosmos/evm/ante/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
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

	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

func TestStandardMsgSendClassifierRawTxBytesBoundary(t *testing.T) {
	configureStandardMsgSendTestDenom()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	classifier := NewStandardMsgSendClassifier(&standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
	})
	tx := newStandardMsgSendTestTx(t, addressCodec, payer)

	for _, test := range []struct {
		name         string
		rawSize      int
		wantEligible bool
	}{
		{name: "2047 bytes", rawSize: StandardMsgSendMaxTxBytes - 1, wantEligible: true},
		{name: "2048 bytes", rawSize: StandardMsgSendMaxTxBytes, wantEligible: true},
		{name: "2049 bytes", rawSize: StandardMsgSendMaxTxBytes + 1, wantEligible: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := sdk.Context{}.
				WithContext(context.Background()).
				WithTxBytes(bytes.Repeat([]byte{0x01}, test.rawSize))
			_, eligible, err := classifier.classify(ctx, tx, false)
			require.NoError(t, err)
			require.Equal(t, test.wantEligible, eligible)
		})
	}
}

func TestStandardMsgSendClassifierGasAndSignaturePolicy(t *testing.T) {
	configureStandardMsgSendTestDenom()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	classifier := NewStandardMsgSendClassifier(&standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
	})
	ethPrivateKey := &ethsecp256k1.PrivKey{Key: bytes.Repeat([]byte{0x01}, 32)}
	ethPublicKey := ethPrivateKey.PubKey()
	require.NotNil(t, ethPublicKey)

	tests := []struct {
		name         string
		simulate     bool
		mutate       func(*standardMsgSendTestTx)
		wantEligible bool
		wantErr      error
	}{
		{
			name:         "secp256k1 single signature at G",
			wantEligible: true,
		},
		{
			name: "ethsecp256k1 single signature at G",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.pubKeys = []cryptotypes.PubKey{ethPublicKey}
				tx.signatures[0].PubKey = ethPublicKey
			},
			wantEligible: true,
		},
		{
			name: "actual transaction below G",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = StandardMsgSendGas - 1
			},
			wantErr: sdkerrors.ErrInvalidGasLimit,
		},
		{
			name: "negative fee is a deterministic admission error",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.fee = sdk.Coins{{
					Denom:  chainconfig.BaseDenom,
					Amount: sdkmath.NewInt(-1),
				}}
			},
			wantErr: sdkerrors.ErrInsufficientFee,
		},
		{
			name:     "simulation D zero is fixed gas-only",
			simulate: true,
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = 0
				tx.fee = sdk.NewCoins()
			},
			wantEligible: true,
		},
		{
			name:     "simulation non-zero D below G is ordinary",
			simulate: true,
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = StandardMsgSendGas - 1
			},
		},
		{
			name:         "simulation D equal to G is fixed",
			simulate:     true,
			wantEligible: true,
		},
		{
			name:     "simulation D above G is fixed",
			simulate: true,
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = 200_000
			},
			wantEligible: true,
		},
		{
			name: "multisig signature data is ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signatures[0].Data = &signing.MultiSignatureData{}
			},
		},
		{
			name: "unsupported public key is ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				publicKey := ed25519.GenPrivKey().PubKey()
				tx.pubKeys = []cryptotypes.PubKey{publicKey}
				tx.signatures[0].PubKey = publicKey
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := newStandardMsgSendTestTx(t, addressCodec, payer)
			if test.mutate != nil {
				test.mutate(&tx)
			}

			_, eligible, err := classifier.classify(
				sdk.Context{}.
					WithContext(context.Background()).
					WithTxBytes(bytes.Repeat([]byte{0x02}, 512)),
				tx,
				test.simulate,
			)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				require.False(t, eligible)
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.wantEligible, eligible)
			if test.wantEligible {
				_, single := tx.signatures[0].Data.(*signing.SingleSignatureData)
				require.True(t, single)
			}
		})
	}
}

func TestStandardMsgSendClassifierExtensionEnvelope(t *testing.T) {
	configureStandardMsgSendTestDenom()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	classifier := NewStandardMsgSendClassifier(&standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
	})
	validDynamic, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{})
	require.NoError(t, err)
	malformedDynamic := &codectypes.Any{TypeUrl: dynamicFeeExtensionURL}
	unknownCritical := &codectypes.Any{TypeUrl: "/guru.test.UnknownCritical"}
	nonCritical := &codectypes.Any{TypeUrl: "/guru.test.NonCritical"}

	tests := []struct {
		name         string
		extensions   []*codectypes.Any
		nonCritical  []*codectypes.Any
		wantEligible bool
		wantErr      error
	}{
		{
			name:         "one approved dynamic extension",
			extensions:   []*codectypes.Any{validDynamic},
			wantEligible: true,
		},
		{
			name:       "duplicate approved dynamic extensions",
			extensions: []*codectypes.Any{validDynamic, validDynamic},
			wantErr:    sdkerrors.ErrUnknownExtensionOptions,
		},
		{
			name:       "unknown critical extension",
			extensions: []*codectypes.Any{unknownCritical},
			wantErr:    sdkerrors.ErrUnknownExtensionOptions,
		},
		{
			name:       "malformed approved dynamic extension",
			extensions: []*codectypes.Any{malformedDynamic},
			wantErr:    sdkerrors.ErrUnknownExtensionOptions,
		},
		{
			name:        "noncritical extension uses ordinary path",
			nonCritical: []*codectypes.Any{nonCritical},
		},
		{
			name:        "approved dynamic plus noncritical uses ordinary path",
			extensions:  []*codectypes.Any{validDynamic},
			nonCritical: []*codectypes.Any{nonCritical},
		},
		{
			name:        "duplicate critical is not hidden by noncritical fallback",
			extensions:  []*codectypes.Any{validDynamic, validDynamic},
			nonCritical: []*codectypes.Any{nonCritical},
			wantErr:     sdkerrors.ErrUnknownExtensionOptions,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := newStandardMsgSendTestTx(t, addressCodec, payer)
			tx.extensions = test.extensions
			tx.nonCriticalExtensions = test.nonCritical
			ctx := sdk.Context{}.
				WithContext(context.Background()).
				WithTxBytes(bytes.Repeat([]byte{0x03}, 512))

			_, eligible, classifyErr := classifier.classify(ctx, tx, false)
			if test.wantErr != nil {
				require.ErrorIs(t, classifyErr, test.wantErr)
				require.False(t, eligible)
				return
			}
			require.NoError(t, classifyErr)
			require.Equal(t, test.wantEligible, eligible)
		})
	}
}

func TestStandardMsgSendClassifierCanonicalShapeBoundary(t *testing.T) {
	configureStandardMsgSendTestDenom()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x34}, 20))
	other := sdk.AccAddress(bytes.Repeat([]byte{0x35}, 20))
	classifier := NewStandardMsgSendClassifier(&standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
	})

	tests := []struct {
		name         string
		mutate       func(*standardMsgSendTestTx)
		wantEligible bool
		wantErr      error
	}{
		{name: "canonical shape", wantEligible: true},
		{
			name: "non-native transfer is ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).Amount = sdk.NewCoins(sdk.NewInt64Coin("uother", 1))
			},
		},
		{
			name: "multiple transfer amounts are ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).Amount = sdk.NewCoins(
					sdk.NewInt64Coin(chainconfig.BaseDenom, 1),
					sdk.NewInt64Coin("uother", 1),
				)
			},
		},
		{
			name: "non-native fee is ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.fee = sdk.NewCoins(sdk.NewInt64Coin("uother", 1))
			},
		},
		{
			name: "empty actual fee is ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.fee = sdk.NewCoins()
			},
		},
		{
			name: "fee grant is ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.granter = append(sdk.AccAddress(nil), payer...)
			},
		},
		{
			name: "different fee payer is ordinary",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.payer = append(sdk.AccAddress(nil), other...)
				tx.signers = [][]byte{append([]byte(nil), other...)}
			},
		},
		{
			name: "invalid native transfer is a standard coin error",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).Amount = sdk.Coins{{
					Denom:  chainconfig.BaseDenom,
					Amount: sdkmath.ZeroInt(),
				}}
			},
			wantErr: sdkerrors.ErrInvalidCoins,
		},
		{
			name: "invalid non-native fee is still a standard fee error",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.fee = sdk.Coins{{Denom: "uother", Amount: sdkmath.ZeroInt()}}
			},
			wantErr: sdkerrors.ErrInsufficientFee,
		},
		{
			name: "memo above auth parameter is rejected",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.memo = strings.Repeat("m", int(authtypes.DefaultParams().MaxMemoCharacters)+1)
			},
			wantErr: sdkerrors.ErrMemoTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := newStandardMsgSendTestTx(t, addressCodec, payer)
			if test.mutate != nil {
				test.mutate(&tx)
			}
			ctx := sdk.Context{}.
				WithContext(context.Background()).
				WithTxBytes(bytes.Repeat([]byte{0x04}, 512))

			_, eligible, classifyErr := classifier.classify(ctx, tx, false)
			if test.wantErr != nil {
				require.ErrorIs(t, classifyErr, test.wantErr)
				require.False(t, eligible)
				return
			}
			require.NoError(t, classifyErr)
			require.Equal(t, test.wantEligible, eligible)
		})
	}
}

func TestProspectiveFeeMarketParamsAdapterUsesTargetHeightBaseFee(t *testing.T) {
	storedBaseFee := sdkmath.LegacyMustNewDecFromStr("11.25")
	prospectiveBaseFee := sdkmath.LegacyMustNewDecFromStr("12.5")
	params := feemarkettypes.DefaultParams()
	params.BaseFee = storedBaseFee
	keeper := &prospectiveFeeMarketKeeperStub{
		params:         params,
		prospectiveFee: prospectiveBaseFee,
	}
	adapter := NewProspectiveFeeMarketParamsAdapter(keeper)

	tests := []struct {
		name             string
		mode             sdk.ExecMode
		wantBaseFee      sdkmath.LegacyDec
		wantCalculations int
	}{
		{name: "CheckTx keeps stored base fee", mode: sdk.ExecModeCheck, wantBaseFee: storedBaseFee},
		{name: "ReCheckTx keeps stored base fee", mode: sdk.ExecModeReCheck, wantBaseFee: storedBaseFee},
		{name: "Simulate keeps stored base fee", mode: sdk.ExecModeSimulate, wantBaseFee: storedBaseFee},
		{
			name:             "PrepareProposal uses prospective base fee",
			mode:             sdk.ExecModePrepareProposal,
			wantBaseFee:      prospectiveBaseFee,
			wantCalculations: 1,
		},
		{
			name:             "ProcessProposal uses prospective base fee",
			mode:             sdk.ExecModeProcessProposal,
			wantBaseFee:      prospectiveBaseFee,
			wantCalculations: 1,
		},
		{name: "FinalizeBlock keeps stored base fee", mode: sdk.ExecModeFinalize, wantBaseFee: storedBaseFee},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			keeper.calculateCalls = 0
			got := adapter.GetParams(sdk.Context{}.
				WithContext(context.Background()).
				WithExecMode(test.mode))
			require.True(t, got.BaseFee.Equal(test.wantBaseFee))
			require.Equal(t, test.wantCalculations, keeper.calculateCalls)
		})
	}

	t.Run("nil prospective base fee preserves the stored value", func(t *testing.T) {
		keeper := &prospectiveFeeMarketKeeperStub{
			params:         params,
			prospectiveFee: sdkmath.LegacyDec{},
		}
		adapter := NewProspectiveFeeMarketParamsAdapter(keeper)
		got := adapter.GetParams(sdk.Context{}.WithExecMode(sdk.ExecModeProcessProposal))
		require.True(t, got.BaseFee.Equal(storedBaseFee))
		require.Equal(t, 1, keeper.calculateCalls)
	})
}

func TestStandardMsgSendGasWantedAddsExactlyGOnlyAfterFinalizeAnteSuccess(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	params := feemarkettypes.DefaultParams()
	params.NoBaseFee = false
	params.EnableHeight = 0
	keeper := &standardMsgSendGasWantedKeeperStub{}
	decorator := newStandardMsgSendGasWantedDecorator(
		evmanteevm.GasWantedDecorator{},
		keeper,
		&params,
	)
	classification := &standardMsgSendClassification{}
	newFixedContext := func(mode sdk.ExecMode) sdk.Context {
		return sdk.Context{}.
			WithContext(context.Background()).
			WithBlockHeight(1).
			WithExecMode(mode).
			WithGasMeter(newStandardMsgSendGasMeter(storetypes.NewInfiniteGasMeter())).
			WithValue(standardMsgSendContextKey{}, classification)
	}

	_, err := decorator.AnteHandle(
		newFixedContext(sdk.ExecModeFinalize),
		nil,
		false,
		func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return nextCtx, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, keeper.calls)
	require.Equal(t, StandardMsgSendGas, keeper.gasWanted)

	keeper.calls = 0
	keeper.gasWanted = 0
	for _, mode := range []sdk.ExecMode{
		sdk.ExecModeCheck,
		sdk.ExecModeReCheck,
		sdk.ExecModeSimulate,
		sdk.ExecModePrepareProposal,
		sdk.ExecModeProcessProposal,
	} {
		_, err = decorator.AnteHandle(
			newFixedContext(mode),
			nil,
			mode == sdk.ExecModeSimulate,
			func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
				return nextCtx, nil
			},
		)
		require.NoError(t, err)
	}
	require.Zero(t, keeper.calls)

	_, err = decorator.AnteHandle(
		newFixedContext(sdk.ExecModeFinalize),
		nil,
		false,
		func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return nextCtx, sdkerrors.ErrUnauthorized
		},
	)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Zero(t, keeper.calls)

	params.NoBaseFee = true
	_, err = decorator.AnteHandle(
		newFixedContext(sdk.ExecModeFinalize),
		nil,
		false,
		func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return nextCtx, nil
		},
	)
	require.NoError(t, err)
	require.Zero(t, keeper.calls)
}

func TestStandardMsgSendMalformedAccessorReturnsTxDecode(t *testing.T) {
	configureStandardMsgSendTestDenom()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	tx := panickingFeePayerTestTx{standardMsgSendTestTx: newStandardMsgSendTestTx(
		t,
		addressCodec,
		payer,
	)}
	decorator := NewStandardMsgSendGasDecorator(&standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
	})
	ctx := sdk.Context{}.
		WithContext(context.Background()).
		WithGasMeter(storetypes.NewInfiniteGasMeter()).
		WithTxBytes(bytes.Repeat([]byte{0x03}, 512))
	nextCalls := 0

	newCtx, err := decorator.AnteHandle(
		ctx,
		tx,
		false,
		func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalls++
			return nextCtx, nil
		},
	)

	require.ErrorIs(t, err, sdkerrors.ErrTxDecode)
	require.Zero(t, nextCalls)
	require.Same(t, ctx.GasMeter(), newCtx.GasMeter())
}

func TestStandardMsgSendGasMeterIsStagedUntilAnteSuccess(t *testing.T) {
	configureStandardMsgSendTestDenom()

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	decorator := NewStandardMsgSendGasDecorator(&standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
	})
	const declaredGas = uint64(200_000)

	for _, test := range []struct {
		name                string
		nextErr             error
		wantConsumedToLimit uint64
	}{
		{
			name:                "successful ante commits G",
			wantConsumedToLimit: StandardMsgSendGas,
		},
		{
			name:    "failed ante leaves block gas at zero",
			nextErr: sdkerrors.ErrUnauthorized,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := newStandardMsgSendTestTx(t, addressCodec, payer)
			tx.gas = declaredGas
			ctx := sdk.Context{}.
				WithContext(context.Background()).
				WithGasMeter(storetypes.NewInfiniteGasMeter()).
				WithTxBytes(bytes.Repeat([]byte{0x04}, 512))

			newCtx, err := decorator.AnteHandle(
				ctx,
				tx,
				false,
				func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
					nextCtx.GasMeter().ConsumeGas(declaredGas+1, "internal ante work")
					return nextCtx, test.nextErr
				},
			)

			if test.nextErr != nil {
				require.ErrorIs(t, err, test.nextErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, declaredGas, tx.GetGas())
			meter, ok := newCtx.GasMeter().(*standardMsgSendGasMeter)
			require.True(t, ok)
			require.Equal(t, StandardMsgSendGas, uint64(meter.Limit()))
			require.Equal(t, StandardMsgSendGas, uint64(meter.GasConsumed()))
			require.Equal(t, test.wantConsumedToLimit, uint64(meter.GasConsumedToLimit()))
			require.Equal(t, declaredGas+1, uint64(meter.actualGasConsumed()))
			require.False(t, meter.IsPastLimit())
			require.False(t, meter.IsOutOfGas())
		})
	}
}

func TestStandardMsgSendFeePlanExactArithmeticAndAffordability(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x66}, 20))
	accountKeeper := &standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
		account:      authtypes.NewBaseAccountWithAddress(payer),
	}
	tx := newStandardMsgSendTestTx(t, addressCodec, payer)
	tx.gas = 42_000
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chainconfig.BaseDenom, 42_001))
	const transferAmount = int64(100)
	tx.msgs[0].(*banktypes.MsgSend).Amount = sdk.NewCoins(
		sdk.NewInt64Coin(chainconfig.BaseDenom, transferAmount),
	)
	ctx := sdk.Context{}.
		WithContext(context.Background()).
		WithBlockHeight(1).
		WithTxBytes(bytes.Repeat([]byte{0x05}, 512))
	classification, eligible, err := NewStandardMsgSendClassifier(accountKeeper).classify(ctx, tx, false)
	require.NoError(t, err)
	require.True(t, eligible)

	params := feemarkettypes.DefaultParams()
	params.NoBaseFee = true
	params.MinGasPrice = sdkmath.LegacyZeroDec()
	plan, err := buildStandardMsgSendFeePlan(
		ctx,
		&classification,
		&params,
		accountKeeper,
		nil,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(42_000), plan.declaredGas)
	require.Equal(t, sdkmath.NewInt(21_001), plan.actualFee.AmountOf(chainconfig.BaseDenom))
	require.Equal(t, "42001", plan.effectivePrice.numerator.String())
	require.Equal(t, "42000", plan.effectivePrice.denominator.String())

	required := sdkmath.NewInt(transferAmount + 42_001)
	for _, test := range []struct {
		name      string
		spendable sdkmath.Int
		wantErr   bool
	}{
		{
			name:      "transfer plus submitted fee exactly passes",
			spendable: required,
		},
		{
			name:      "one below transfer plus submitted fee fails",
			spendable: required.SubRaw(1),
			wantErr:   true,
		},
		{
			name: "transfer plus actual fee is still insufficient",
			spendable: sdkmath.NewInt(transferAmount).Add(
				plan.actualFee.AmountOf(chainconfig.BaseDenom),
			),
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			bankKeeper := &standardMsgSendSpendableBankKeeperStub{
				spendable: test.spendable,
			}
			_, err := buildStandardMsgSendFeePlan(
				ctx,
				&classification,
				&params,
				accountKeeper,
				bankKeeper,
				true,
			)
			if test.wantErr {
				require.ErrorIs(t, err, sdkerrors.ErrInsufficientFunds)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, chainconfig.BaseDenom, bankKeeper.requestedDenom)
		})
	}
}

func TestStandardMsgSendFeePlanBoundedInvariants(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x67}, 20))
	accountKeeper := &standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
		account:      authtypes.NewBaseAccountWithAddress(payer),
	}
	ctx := sdk.Context{}.
		WithContext(context.Background()).
		WithBlockHeight(1).
		WithTxBytes(bytes.Repeat([]byte{0x07}, 512))

	declaredGasCases := []uint64{
		StandardMsgSendGas,
		StandardMsgSendGas + 1,
		42_000,
		200_000,
		^uint64(0),
	}
	priceCases := []int64{1, 2, 17, 1_000_000}

	for _, declaredGas := range declaredGasCases {
		for _, minimumPrice := range priceCases {
			for _, remainder := range []uint64{0, 1, declaredGas - 1} {
				declaredGasInt := new(big.Int).SetUint64(declaredGas)
				submittedFeeInt := new(big.Int).Mul(
					new(big.Int).Set(declaredGasInt),
					big.NewInt(minimumPrice),
				)
				submittedFeeInt.Add(submittedFeeInt, new(big.Int).SetUint64(remainder))

				tx := newStandardMsgSendTestTx(t, addressCodec, payer)
				tx.gas = declaredGas
				tx.fee = sdk.NewCoins(sdk.NewCoin(
					chainconfig.BaseDenom,
					sdkmath.NewIntFromBigInt(submittedFeeInt),
				))
				classification, eligible, err := NewStandardMsgSendClassifier(accountKeeper).classify(
					ctx,
					tx,
					false,
				)
				require.NoError(t, err)
				require.True(t, eligible)

				params := feemarkettypes.DefaultParams()
				params.NoBaseFee = true
				params.MinGasPrice = sdkmath.LegacyNewDec(minimumPrice)
				plan, err := buildStandardMsgSendFeePlan(
					ctx,
					&classification,
					&params,
					accountKeeper,
					nil,
					false,
				)
				require.NoError(t, err)

				expectedActualFee := ceilStandardMsgSendQuotient(
					new(big.Int).Mul(
						new(big.Int).Set(submittedFeeInt),
						new(big.Int).SetUint64(StandardMsgSendGas),
					),
					declaredGasInt,
				)
				actualFee := plan.actualFee.AmountOf(chainconfig.BaseDenom).BigInt()
				minimumActualFee := new(big.Int).Mul(
					big.NewInt(minimumPrice),
					new(big.Int).SetUint64(StandardMsgSendGas),
				)

				require.Zero(t, actualFee.Cmp(expectedActualFee))
				require.LessOrEqual(t, actualFee.Cmp(submittedFeeInt), 0)
				require.GreaterOrEqual(t, actualFee.Cmp(minimumActualFee), 0)
			}
		}
	}
}

func TestStandardMsgSendFeePlanDynamicTipCapAndNegativeTip(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x77}, 20))
	accountKeeper := &standardMsgSendAccountKeeper{
		addressCodec: addressCodec,
		account:      authtypes.NewBaseAccountWithAddress(payer),
	}
	tx := newStandardMsgSendTestTx(t, addressCodec, payer)
	tx.gas = 42_000
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chainconfig.BaseDenom, 84_000))
	extension, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{
		MaxPriorityPrice: sdkmath.LegacyNewDec(10),
	})
	require.NoError(t, err)
	tx.extensions = []*codectypes.Any{extension}

	ctx := sdk.Context{}.
		WithContext(context.Background()).
		WithBlockHeight(1).
		WithTxBytes(bytes.Repeat([]byte{0x06}, 512))
	classification, eligible, err := NewStandardMsgSendClassifier(accountKeeper).classify(ctx, tx, false)
	require.NoError(t, err)
	require.True(t, eligible)
	params := feemarkettypes.DefaultParams()
	params.BaseFee = sdkmath.LegacyOneDec()
	params.MinGasPrice = sdkmath.LegacyZeroDec()

	plan, err := buildStandardMsgSendFeePlan(
		ctx,
		&classification,
		&params,
		accountKeeper,
		nil,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, "2", standardMsgSendPriceString(plan.effectivePrice))
	require.Equal(t, sdkmath.NewInt(42_000), plan.actualFee.AmountOf(chainconfig.BaseDenom))

	negativeTip, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{
		MaxPriorityPrice: sdkmath.LegacyNewDec(-1),
	})
	require.NoError(t, err)
	tx.extensions = []*codectypes.Any{negativeTip}
	negativeClassification, eligible, err := NewStandardMsgSendClassifier(accountKeeper).classify(
		ctx,
		tx,
		false,
	)
	require.NoError(t, err)
	require.True(t, eligible)
	_, err = buildStandardMsgSendFeePlan(
		ctx,
		&negativeClassification,
		&params,
		accountKeeper,
		nil,
		false,
	)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
}

func TestStandardMsgSendDeductFeeUsesActualFeeAndEmitsObservabilityEvent(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)

	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x78}, 20))
	accountKeeper := &dynamicFeeFloorAccountKeeper{
		addressCodec:  addressCodec,
		account:       authtypes.NewBaseAccountWithAddress(payer),
		moduleAddress: sdk.AccAddress(bytes.Repeat([]byte{0x79}, 20)),
	}
	bankKeeper := &dynamicFeeFloorBankKeeper{}
	tx := newStandardMsgSendTestTx(t, addressCodec, payer)
	tx.gas = 42_000
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chainconfig.BaseDenom, 42_001))
	ctx := sdk.Context{}.
		WithContext(context.Background()).
		WithBlockHeight(1).
		WithTxBytes(bytes.Repeat([]byte{0x08}, 512)).
		WithEventManager(sdk.NewEventManager())
	classification, eligible, err := NewStandardMsgSendClassifier(accountKeeper).classify(ctx, tx, false)
	require.NoError(t, err)
	require.True(t, eligible)

	params := feemarkettypes.DefaultParams()
	params.NoBaseFee = true
	params.MinGasPrice = sdkmath.LegacyZeroDec()
	plan, err := buildStandardMsgSendFeePlan(
		ctx,
		&classification,
		&params,
		accountKeeper,
		nil,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(21_001), plan.actualFee.AmountOf(chainconfig.BaseDenom))

	fixedCtx := ctx.
		WithValue(standardMsgSendContextKey{}, &classification).
		WithValue(standardMsgSendFeePlanContextKey{}, &plan)
	decorator := newStandardMsgSendDeductFeeDecorator(
		accountKeeper,
		bankKeeper,
		nil,
		nil,
	)
	nextCalls := 0
	newCtx, err := decorator.AnteHandle(
		fixedCtx,
		tx,
		false,
		func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalls++
			return nextCtx, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalls)
	require.Equal(t, 1, bankKeeper.sendCalls)
	require.Equal(t, payer, bankKeeper.from)
	require.Equal(t, authtypes.FeeCollectorName, bankKeeper.module)
	require.Equal(t, plan.actualFee, bankKeeper.amount)

	var fixedEvent *sdk.Event
	for _, event := range newCtx.EventManager().Events() {
		if event.Type == EventTypeFixedSendGas {
			eventCopy := event
			fixedEvent = &eventCopy
			break
		}
	}
	require.NotNil(t, fixedEvent)
	attributes := make(map[string]string, len(fixedEvent.Attributes))
	for _, attribute := range fixedEvent.Attributes {
		attributes[attribute.Key] = attribute.Value
	}
	require.Equal(t, map[string]string{
		AttributeKeyDeclaredGas:       "42000",
		AttributeKeyAccountingGas:     "21000",
		AttributeKeySubmittedFee:      plan.submittedFee.String(),
		AttributeKeyEffectiveGasPrice: "42001/42000",
		AttributeKeyActualFee:         plan.actualFee.String(),
	}, attributes)
}

func TestAnteRouterPreservesUpstreamErrors(t *testing.T) {
	configureStandardMsgSendFeeTestChain(t, 0)
	addressCodec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x7a}, 20))
	rejectedEthereumTx := newStandardMsgSendTestTx(t, addressCodec, payer)
	rejectedEthereumTx.msgs = []sdk.Msg{&evmtypes.MsgEthereumTx{}}
	rejectedEthereumTx.gas = 123_456

	params := feemarkettypes.DefaultParams()
	options := evmante.HandlerOptions{
		FeeMarketKeeper: &prospectiveFeeMarketKeeperStub{params: params},
	}
	localHandler := NewAnteHandler(options)
	upstreamHandler := evmante.NewAnteHandler(options)
	tests := []struct {
		name               string
		tx                 sdk.Tx
		wantLocalErr       error
		compareReturnedGas bool
	}{
		{name: "nil transaction", wantLocalErr: sdkerrors.ErrTxDecode},
		{
			name: "unknown extension",
			tx: anteRouterTestTx{extensions: []*codectypes.Any{{
				TypeUrl: "/test.unsupported.ExtensionOption",
			}}},
		},
		{
			name:               "rejected Ethereum message preserves pre-setup gas context",
			tx:                 rejectedEthereumTx,
			compareReturnedGas: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := sdk.Context{}.
				WithContext(context.Background()).
				WithGasMeter(storetypes.NewInfiniteGasMeter())
			wantCtx, wantErr := upstreamHandler(ctx, test.tx, false)
			gotCtx, gotErr := localHandler(ctx, test.tx, false)
			require.Error(t, wantErr)
			if test.wantLocalErr != nil {
				require.ErrorIs(t, gotErr, test.wantLocalErr)
			} else {
				require.EqualError(t, gotErr, wantErr.Error())
			}
			if test.compareReturnedGas {
				require.Equal(t, wantCtx.GasMeter().Limit(), gotCtx.GasMeter().Limit())
				require.Equal(t, wantCtx.GasMeter().GasConsumed(), gotCtx.GasMeter().GasConsumed())
			}
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
			name: "unsupported key MsgSend",
			mutate: func(tx *standardMsgSendTestTx) {
				publicKey := ed25519.GenPrivKey().PubKey()
				tx.pubKeys = []cryptotypes.PubKey{publicKey}
				tx.signatures[0].PubKey = publicKey
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
		WithContext(context.Background()).
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

func (*dynamicFeeFloorAccountKeeper) GetParams(context.Context) authtypes.Params {
	return authtypes.DefaultParams()
}

func (keeper *dynamicFeeFloorAccountKeeper) GetModuleAddress(string) sdk.AccAddress {
	return keeper.moduleAddress
}

type dynamicFeeFloorBankKeeper struct {
	authtypes.BankKeeper
	sendCalls int
	from      sdk.AccAddress
	module    string
	amount    sdk.Coins
}

func (keeper *dynamicFeeFloorBankKeeper) SendCoinsFromAccountToModule(
	_ context.Context,
	from sdk.AccAddress,
	module string,
	amount sdk.Coins,
) error {
	keeper.sendCalls++
	keeper.from = append(sdk.AccAddress(nil), from...)
	keeper.module = module
	keeper.amount = amount
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

type standardMsgSendAccountKeeper struct {
	authante.AccountKeeper
	addressCodec coreaddress.Codec
	account      sdk.AccountI
}

func (keeper *standardMsgSendAccountKeeper) AddressCodec() coreaddress.Codec {
	return keeper.addressCodec
}

func (*standardMsgSendAccountKeeper) GetParams(context.Context) authtypes.Params {
	return authtypes.DefaultParams()
}

func (keeper *standardMsgSendAccountKeeper) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI {
	return keeper.account
}

type standardMsgSendSpendableBankKeeperStub struct {
	spendable      sdkmath.Int
	requestedDenom string
}

func (keeper *standardMsgSendSpendableBankKeeperStub) SpendableCoin(
	_ context.Context,
	_ sdk.AccAddress,
	denom string,
) sdk.Coin {
	keeper.requestedDenom = denom
	return sdk.NewCoin(denom, keeper.spendable)
}

type prospectiveFeeMarketKeeperStub struct {
	params         feemarkettypes.Params
	prospectiveFee sdkmath.LegacyDec
	calculateCalls int
}

func (keeper *prospectiveFeeMarketKeeperStub) GetParams(sdk.Context) feemarkettypes.Params {
	return keeper.params
}

func (*prospectiveFeeMarketKeeperStub) AddTransientGasWanted(
	sdk.Context,
	uint64,
) (uint64, error) {
	return 0, nil
}

func (keeper *prospectiveFeeMarketKeeperStub) CalculateBaseFee(sdk.Context) sdkmath.LegacyDec {
	keeper.calculateCalls++
	return keeper.prospectiveFee
}

type standardMsgSendGasWantedKeeperStub struct {
	calls     int
	gasWanted uint64
}

func (keeper *standardMsgSendGasWantedKeeperStub) AddTransientGasWanted(
	_ sdk.Context,
	gasWanted uint64,
) (uint64, error) {
	keeper.calls++
	keeper.gasWanted += gasWanted
	return keeper.gasWanted, nil
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
			Amount:      sdk.NewCoins(sdk.NewInt64Coin(chainconfig.BaseDenom, 1)),
		}},
		fee:     sdk.NewCoins(sdk.NewInt64Coin(chainconfig.BaseDenom, 1)),
		gas:     StandardMsgSendGas,
		payer:   append(sdk.AccAddress(nil), payer...),
		signers: [][]byte{append([]byte(nil), payer...)},
		pubKeys: []cryptotypes.PubKey{pubKey},
		signatures: []signing.SignatureV2{{
			PubKey: pubKey,
			Data: &signing.SingleSignatureData{
				SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
				Signature: []byte{0x01},
			},
		}},
	}
}

func configureStandardMsgSendTestDenom() {
	evmtypes.SetDefaultEvmCoinInfo(evmtypes.EvmCoinInfo{
		Denom:         chainconfig.BaseDenom,
		ExtendedDenom: chainconfig.BaseDenom,
		DisplayDenom:  chainconfig.DisplayDenom,
		Decimals:      18,
	})
}
