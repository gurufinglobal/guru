package ante

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cosmosevmante "github.com/cosmos/evm/ante/evm"
	cosmosevmtypes "github.com/cosmos/evm/ante/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ethparams "github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	protov2 "google.golang.org/protobuf/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkauthante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authantetestutil "github.com/cosmos/cosmos-sdk/x/auth/ante/testutil"
	authtestutil "github.com/cosmos/cosmos-sdk/x/auth/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

type noCallFeePolicyKeeper struct {
	t *testing.T
}

func (keeper noCallFeePolicyKeeper) ResolveDiscount(
	context.Context,
	string,
	[]sdk.Msg,
) (feepolicytypes.Discount, error) {
	keeper.t.Helper()
	keeper.t.Fatal("fee policy lookup must not run")
	return feepolicytypes.Discount{}, nil
}

type staticFeePolicyKeeper struct {
	discount feepolicytypes.Discount
}

func (keeper staticFeePolicyKeeper) ResolveDiscount(
	context.Context,
	string,
	[]sdk.Msg,
) (feepolicytypes.Discount, error) {
	return keeper.discount, nil
}

type virtualFeeBankKeeperSpy struct {
	calls           int
	ctx             context.Context
	senderAddr      sdk.AccAddress
	recipientModule string
	amount          sdk.Coins
	err             error
	onSend          func(context.Context, sdk.AccAddress, string, sdk.Coins) error
}

func (keeper *virtualFeeBankKeeperSpy) SendCoinsFromAccountToModuleVirtual(
	ctx context.Context,
	senderAddr sdk.AccAddress,
	recipientModule string,
	amount sdk.Coins,
) error {
	keeper.calls++
	keeper.ctx = ctx
	keeper.senderAddr = senderAddr
	keeper.recipientModule = recipientModule
	keeper.amount = amount
	if keeper.onSend != nil {
		return keeper.onSend(ctx, senderAddr, recipientModule, amount)
	}

	return keeper.err
}

func feeDecoratorTestPayer() sdk.AccAddress {
	return sdk.AccAddress([]byte("fee-policy-payer-001"))
}

func staticFeeBreakdownChecker(fee sdk.Coins, priority int64) TxFeeChecker {
	return func(sdk.Context, sdk.Tx) (EffectiveFeeBreakdown, error) {
		return EffectiveFeeBreakdown{
			EffectiveFee:  fee,
			BaseComponent: fee,
			TipComponent:  sdk.Coins{},
			Priority:      priority,
		}, nil
	}
}

func TestApplyDiscountToBaseFee(t *testing.T) {
	baseFee := sdk.NewCoins(
		sdk.NewInt64Coin("ubar", 11),
		sdk.NewInt64Coin("ufoo", 101),
	)

	tests := []struct {
		name     string
		discount feepolicytypes.Discount
		expected sdk.Coins
	}{
		{
			name: "percent 25 truncates each denom",
			discount: feepolicytypes.Discount{
				DiscountType: feepolicytypes.FeeDiscountTypePercent,
				Amount:       sdkmath.LegacyNewDec(25),
			},
			expected: sdk.NewCoins(sdk.NewInt64Coin("ubar", 8), sdk.NewInt64Coin("ufoo", 75)),
		},
		{
			name: "fixed is final base charge",
			discount: feepolicytypes.Discount{
				DiscountType: feepolicytypes.FeeDiscountTypeFixed,
				Amount:       sdkmath.LegacyNewDecWithPrec(49, 1),
			},
			expected: sdk.NewCoins(sdk.NewInt64Coin("ubar", 4), sdk.NewInt64Coin("ufoo", 4)),
		},
		{
			name: "fixed is capped at original",
			discount: feepolicytypes.Discount{
				DiscountType: feepolicytypes.FeeDiscountTypeFixed,
				Amount:       sdkmath.LegacyNewDec(1_000),
			},
			expected: baseFee,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := applyDiscountToBaseFee(baseFee, test.discount)
			require.NoError(t, err)
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestDeductFeeDecoratorNoPolicyMatchesUpstreamFeeAmountPriorityAndEvents(t *testing.T) {
	payer := feeDecoratorTestPayer()
	fee := parityFee(200)
	tx := parityFeeTx{gas: 10, fee: fee, payer: payer}
	params := parityFeeMarketParams("10")
	ethConfig := parityLondonConfig(0)

	type observation struct {
		payerBalance sdkmath.Int
		collectedFee sdkmath.Int
		priority     int64
		events       sdk.Events
	}
	run := func(local bool) observation {
		controller := gomock.NewController(t)
		accountKeeper := authantetestutil.NewMockAccountKeeper(controller)
		bankKeeper := authtestutil.NewMockBankKeeper(controller)
		accountKeeper.EXPECT().
			GetModuleAddress(authtypes.FeeCollectorName).
			Return(authtypes.NewModuleAddress(authtypes.FeeCollectorName))
		accountKeeper.EXPECT().
			GetAccount(gomock.Any(), payer).
			Return(authtypes.NewBaseAccountWithAddress(payer))
		if local {
			accountKeeper.EXPECT().
				AddressCodec().
				Return(evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()))
		}

		payerBalance := sdkmath.NewInt(1_000)
		collectedFee := sdkmath.ZeroInt()
		recordCollection := func(_ context.Context, _ sdk.AccAddress, _ string, amount sdk.Coins) error {
			payerBalance = payerBalance.Sub(amount.AmountOf(parityFeeDenom))
			collectedFee = collectedFee.Add(amount.AmountOf(parityFeeDenom))
			return nil
		}
		virtualBankKeeper := &virtualFeeBankKeeperSpy{onSend: recordCollection}
		if !local {
			bankKeeper.EXPECT().
				SendCoinsFromAccountToModule(gomock.Any(), payer, authtypes.FeeCollectorName, fee).
				DoAndReturn(func(ctx context.Context, sender sdk.AccAddress, module string, amount sdk.Coins) error {
					return recordCollection(ctx, sender, module, amount)
				})
		}

		ctx := parityFeeContext(1, false)
		var (
			newCtx sdk.Context
			err    error
		)
		if local {
			checker := func(ctx sdk.Context, tx sdk.Tx) (EffectiveFeeBreakdown, error) {
				return FeeChecker(ctx, &params, parityFeeDenom, ethConfig, tx.(sdk.FeeTx))
			}
			newCtx, err = NewDeductFeeDecorator(
				accountKeeper,
				virtualBankKeeper.SendCoinsFromAccountToModuleVirtual,
				nil,
				checker,
				staticFeePolicyKeeper{},
			).AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
				return ctx, nil
			})
		} else {
			checker := func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
				return cosmosevmante.FeeChecker(ctx, &params, parityFeeDenom, ethConfig, tx.(sdk.FeeTx))
			}
			newCtx, err = sdkauthante.NewDeductFeeDecorator(
				accountKeeper,
				bankKeeper,
				nil,
				checker,
			).AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
				return ctx, nil
			})
		}
		require.NoError(t, err)
		if local {
			require.Equal(t, 1, virtualBankKeeper.calls)
			require.Equal(t, payer, virtualBankKeeper.senderAddr)
			require.Equal(t, authtypes.FeeCollectorName, virtualBankKeeper.recipientModule)
			require.Equal(t, fee, virtualBankKeeper.amount)
		}
		return observation{
			payerBalance: payerBalance,
			collectedFee: collectedFee,
			priority:     newCtx.Priority(),
			events:       newCtx.EventManager().Events(),
		}
	}

	local := run(true)
	upstream := run(false)
	require.Equal(t, sdkmath.NewInt(800), local.payerBalance)
	require.Equal(t, sdkmath.NewInt(200), local.collectedFee)
	require.Equal(t, upstream, local)
	require.Len(t, local.events, 1)
}

func TestDeductFeeDecoratorPreservesCheckerPriority(t *testing.T) {
	controller := gomock.NewController(t)
	accountKeeper := authantetestutil.NewMockAccountKeeper(controller)
	virtualBankKeeper := &virtualFeeBankKeeperSpy{}
	payer := feeDecoratorTestPayer()
	accountKeeper.EXPECT().
		GetModuleAddress(authtypes.FeeCollectorName).
		Return(authtypes.NewModuleAddress(authtypes.FeeCollectorName))
	accountKeeper.EXPECT().
		AddressCodec().
		Return(evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()))
	accountKeeper.EXPECT().
		GetAccount(gomock.Any(), payer).
		Return(authtypes.NewBaseAccountWithAddress(payer))

	const priority = int64(73)
	nextPriority := int64(0)
	fee := parityFee(60)
	ctx, err := NewDeductFeeDecorator(
		accountKeeper,
		virtualBankKeeper.SendCoinsFromAccountToModuleVirtual,
		nil,
		staticFeeBreakdownChecker(fee, priority),
		staticFeePolicyKeeper{discount: feepolicytypes.Discount{
			DiscountType: feepolicytypes.FeeDiscountTypePercent,
			Amount:       sdkmath.LegacyNewDec(100),
		}},
	).AnteHandle(
		parityFeeContext(1, false),
		parityFeeTx{gas: 1, fee: fee, payer: payer},
		false,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextPriority = ctx.Priority()
			return ctx, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, priority, nextPriority)
	require.Equal(t, priority, ctx.Priority())
	require.Zero(t, virtualBankKeeper.calls, "zero fees must not create a virtual collection entry")
	require.Len(t, ctx.EventManager().Events(), 1)
	require.Equal(t, sdk.Coins{}.String(), ctx.EventManager().Events()[0].Attributes[0].Value)
}

func TestDeductFeeDecoratorMissingFeeCollectorFailsBeforeSideEffects(t *testing.T) {
	controller := gomock.NewController(t)
	accountKeeper := authantetestutil.NewMockAccountKeeper(controller)
	accountKeeper.EXPECT().GetModuleAddress(authtypes.FeeCollectorName).Return(nil)
	virtualBankKeeper := &virtualFeeBankKeeperSpy{}
	nextCalled := false
	ctx := parityFeeContext(1, false)

	_, err := NewDeductFeeDecorator(
		accountKeeper,
		virtualBankKeeper.SendCoinsFromAccountToModuleVirtual,
		nil,
		staticFeeBreakdownChecker(parityFee(60), 1),
		noCallFeePolicyKeeper{t: t},
	).AnteHandle(
		ctx,
		parityFeeTx{gas: 1, fee: parityFee(60), payer: feeDecoratorTestPayer()},
		false,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			return ctx, nil
		},
	)

	require.ErrorContains(t, err, "fee recipient module account")
	require.False(t, nextCalled)
	require.Empty(t, ctx.EventManager().Events())
}

func TestDeductFeeDecoratorMissingPayerFailsBeforeBankSideEffects(t *testing.T) {
	controller := gomock.NewController(t)
	accountKeeper := authantetestutil.NewMockAccountKeeper(controller)
	virtualBankKeeper := &virtualFeeBankKeeperSpy{}
	payer := feeDecoratorTestPayer()
	accountKeeper.EXPECT().
		GetModuleAddress(authtypes.FeeCollectorName).
		Return(authtypes.NewModuleAddress(authtypes.FeeCollectorName))
	accountKeeper.EXPECT().
		AddressCodec().
		Return(evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()))
	accountKeeper.EXPECT().GetAccount(gomock.Any(), payer).Return(nil)
	nextCalled := false
	ctx := parityFeeContext(1, false)

	_, err := NewDeductFeeDecorator(
		accountKeeper,
		virtualBankKeeper.SendCoinsFromAccountToModuleVirtual,
		nil,
		staticFeeBreakdownChecker(parityFee(60), 1),
		staticFeePolicyKeeper{},
	).AnteHandle(
		ctx,
		parityFeeTx{gas: 1, fee: parityFee(60), payer: payer},
		false,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			return ctx, nil
		},
	)

	require.ErrorIs(t, err, sdkerrors.ErrUnknownAddress)
	require.False(t, nextCalled)
	require.Empty(t, ctx.EventManager().Events())
}

func TestDeductFeeDecoratorVirtualCollectionFailureStopsAnteChain(t *testing.T) {
	controller := gomock.NewController(t)
	accountKeeper := authantetestutil.NewMockAccountKeeper(controller)
	payer := feeDecoratorTestPayer()
	fee := parityFee(60)
	virtualBankKeeper := &virtualFeeBankKeeperSpy{err: errors.New("virtual collection failed")}
	accountKeeper.EXPECT().
		GetModuleAddress(authtypes.FeeCollectorName).
		Return(authtypes.NewModuleAddress(authtypes.FeeCollectorName))
	accountKeeper.EXPECT().
		AddressCodec().
		Return(evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()))
	accountKeeper.EXPECT().
		GetAccount(gomock.Any(), payer).
		Return(authtypes.NewBaseAccountWithAddress(payer))

	nextCalled := false
	ctx := parityFeeContext(1, false)
	_, err := NewDeductFeeDecorator(
		accountKeeper,
		virtualBankKeeper.SendCoinsFromAccountToModuleVirtual,
		nil,
		staticFeeBreakdownChecker(fee, 1),
		staticFeePolicyKeeper{},
	).AnteHandle(
		ctx,
		parityFeeTx{gas: 1, fee: fee, payer: payer},
		false,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			return ctx, nil
		},
	)

	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFunds)
	require.ErrorContains(t, err, "virtual collection failed")
	require.Equal(t, 1, virtualBankKeeper.calls)
	require.Equal(t, payer, virtualBankKeeper.senderAddr)
	require.Equal(t, authtypes.FeeCollectorName, virtualBankKeeper.recipientModule)
	require.Equal(t, fee, virtualBankKeeper.amount)
	require.False(t, nextCalled)
	require.Empty(t, ctx.EventManager().Events())
}

func TestDeductFeeDecoratorRejectsInvalidCheckerBreakdownBeforeSideEffects(t *testing.T) {
	validEffective := parityFee(60)
	validBase := parityFee(50)
	validTip := parityFee(10)
	invalidCoins := sdk.Coins{{
		Denom:  parityFeeDenom,
		Amount: sdkmath.ZeroInt(),
	}}

	tests := []struct {
		name      string
		breakdown EffectiveFeeBreakdown
	}{
		{
			name: "invalid effective fee",
			breakdown: EffectiveFeeBreakdown{
				EffectiveFee:  invalidCoins,
				BaseComponent: validBase,
				TipComponent:  validTip,
			},
		},
		{
			name: "invalid base component",
			breakdown: EffectiveFeeBreakdown{
				EffectiveFee:  validEffective,
				BaseComponent: invalidCoins,
				TipComponent:  validTip,
			},
		},
		{
			name: "invalid tip component",
			breakdown: EffectiveFeeBreakdown{
				EffectiveFee:  validEffective,
				BaseComponent: validBase,
				TipComponent:  invalidCoins,
			},
		},
		{
			name: "inconsistent components",
			breakdown: EffectiveFeeBreakdown{
				EffectiveFee:  validEffective,
				BaseComponent: validBase,
				TipComponent:  parityFee(9),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := gomock.NewController(t)
			decorator := NewDeductFeeDecorator(
				authantetestutil.NewMockAccountKeeper(controller),
				new(virtualFeeBankKeeperSpy).SendCoinsFromAccountToModuleVirtual,
				authantetestutil.NewMockFeegrantKeeper(controller),
				func(sdk.Context, sdk.Tx) (EffectiveFeeBreakdown, error) {
					return test.breakdown, nil
				},
				noCallFeePolicyKeeper{t: t},
			)
			tx := parityFeeTx{
				gas:   1,
				fee:   validEffective,
				payer: sdk.AccAddress{1},
			}

			nextCalled := false
			ctx := parityFeeContext(1, false)
			_, err := decorator.AnteHandle(
				ctx,
				tx,
				false,
				func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
					nextCalled = true
					return ctx, nil
				},
			)

			require.ErrorIs(t, err, sdkerrors.ErrLogic)
			require.False(t, nextCalled)
			require.Empty(t, ctx.EventManager().Events())
		})
	}
}

const parityFeeDenom = "aguru"

func parityFee(amount int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewInt64Coin(parityFeeDenom, amount))
}

type parityFeeTx struct {
	msgs       []sdk.Msg
	gas        uint64
	fee        sdk.Coins
	payer      sdk.AccAddress
	granter    sdk.AccAddress
	extensions []*codectypes.Any
}

func (tx parityFeeTx) GetMsgs() []sdk.Msg { return tx.msgs }

func (tx parityFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx parityFeeTx) GetGas() uint64 { return tx.gas }

func (tx parityFeeTx) GetFee() sdk.Coins { return tx.fee }

func (tx parityFeeTx) FeePayer() []byte { return tx.payer }

func (tx parityFeeTx) FeeGranter() []byte { return tx.granter }

func (tx parityFeeTx) GetExtensionOptions() []*codectypes.Any { return tx.extensions }

func (parityFeeTx) GetNonCriticalExtensionOptions() []*codectypes.Any { return nil }

func parityDynamicOption(t *testing.T, maxPriorityPrice sdkmath.LegacyDec) *codectypes.Any {
	t.Helper()

	option, err := codectypes.NewAnyWithValue(&cosmosevmtypes.ExtensionOptionDynamicFeeTx{
		MaxPriorityPrice: maxPriorityPrice,
	})
	require.NoError(t, err)
	return option
}

func parityFeeContext(height int64, checkTx bool) sdk.Context {
	return sdk.NewContext(
		nil,
		cmtproto.Header{Height: height},
		checkTx,
		log.NewNopLogger(),
	)
}

func parityFeeMarketParams(baseFee string) feemarkettypes.Params {
	params := feemarkettypes.DefaultParams()
	params.BaseFee = sdkmath.LegacyMustNewDecFromStr(baseFee)
	params.EnableHeight = 0
	return params
}

func parityLondonConfig(block int64) *ethparams.ChainConfig {
	return &ethparams.ChainConfig{LondonBlock: big.NewInt(block)}
}

func requireFeeCheckerParity(
	t *testing.T,
	ctx sdk.Context,
	params *feemarkettypes.Params,
	ethConfig *ethparams.ChainConfig,
	tx parityFeeTx,
) EffectiveFeeBreakdown {
	t.Helper()

	local, localErr := FeeChecker(ctx, params, parityFeeDenom, ethConfig, tx)
	upstreamFee, upstreamPriority, upstreamErr := cosmosevmante.FeeChecker(
		ctx,
		params,
		parityFeeDenom,
		ethConfig,
		tx,
	)
	require.NoError(t, localErr)
	require.NoError(t, upstreamErr)
	require.Equal(t, upstreamFee, local.EffectiveFee)
	require.Equal(t, upstreamPriority, local.Priority)
	require.Equal(t, local.EffectiveFee, local.BaseComponent.Add(local.TipComponent...))
	return local
}

func TestDynamicFeeCheckerCosmosEVMV070Parity(t *testing.T) {
	t.Run("effective fee and nominal priority", func(t *testing.T) {
		params := parityFeeMarketParams("10")
		tip := sdkmath.LegacyNewDec(5).MulInt(evmtypes.DefaultPriorityReduction)
		fee := sdkmath.NewInt(10).Add(evmtypes.DefaultPriorityReduction.MulRaw(5))
		tx := parityFeeTx{
			gas:        1,
			fee:        sdk.NewCoins(sdk.NewCoin(parityFeeDenom, fee)),
			extensions: []*codectypes.Any{parityDynamicOption(t, tip)},
		}

		got := requireFeeCheckerParity(t, parityFeeContext(1, false), &params, parityLondonConfig(0), tx)
		require.Equal(t, parityFee(10), got.BaseComponent)
		require.Equal(
			t,
			sdk.NewCoins(sdk.NewCoin(parityFeeDenom, evmtypes.DefaultPriorityReduction.MulRaw(5))),
			got.TipComponent,
		)
		require.Equal(t, int64(5), got.Priority)
	})

	t.Run("London normal Cosmos transaction defaults the tip cap", func(t *testing.T) {
		params := parityFeeMarketParams("10")
		tx := parityFeeTx{
			gas: 2,
			fee: parityFee(30),
		}

		got := requireFeeCheckerParity(t, parityFeeContext(1, false), &params, parityLondonConfig(0), tx)
		require.Equal(t, parityFee(30), got.EffectiveFee)
		require.Equal(t, parityFee(20), got.BaseComponent)
		require.Equal(t, parityFee(10), got.TipComponent)
	})

	t.Run("base and tip use independent ceil rounding", func(t *testing.T) {
		params := parityFeeMarketParams("0.4")
		tx := parityFeeTx{
			gas:        3,
			fee:        parityFee(3),
			extensions: []*codectypes.Any{parityDynamicOption(t, sdkmath.LegacyMustNewDecFromStr("0.2"))},
		}

		got := requireFeeCheckerParity(t, parityFeeContext(1, false), &params, parityLondonConfig(0), tx)
		require.Equal(t, parityFee(2), got.EffectiveFee)
		require.Equal(t, parityFee(1), got.BaseComponent)
		require.Equal(t, parityFee(1), got.TipComponent)

		discounted, err := applyDiscountToBaseFee(got.BaseComponent, feepolicytypes.Discount{
			DiscountType: feepolicytypes.FeeDiscountTypePercent,
			Amount:       sdkmath.LegacyNewDec(50),
		})
		require.NoError(t, err)
		require.Equal(
			t,
			parityFee(1),
			discounted.Add(got.TipComponent...),
			"rounding base and tip separately before deriving base would overcharge this case",
		)
	})

	t.Run("disabled base fee preserves the zero-base special case", func(t *testing.T) {
		params := parityFeeMarketParams("9")
		params.NoBaseFee = true
		tx := parityFeeTx{
			gas:        3,
			fee:        parityFee(3),
			extensions: []*codectypes.Any{parityDynamicOption(t, sdkmath.LegacyMustNewDecFromStr("0.2"))},
		}

		got := requireFeeCheckerParity(t, parityFeeContext(1, false), &params, parityLondonConfig(0), tx)
		require.Equal(t, parityFee(1), got.EffectiveFee)
		require.Equal(t, got.EffectiveFee, got.BaseComponent)
		require.True(t, got.TipComponent.IsZero())
	})
}

func TestDynamicFeeCheckerBlockZeroFallbackParity(t *testing.T) {
	params := parityFeeMarketParams("1000000")
	fee := sdk.NewCoins(sdk.NewCoin(parityFeeDenom, evmtypes.DefaultPriorityReduction.MulRaw(6)))
	tx := parityFeeTx{gas: 2, fee: fee}
	ctx := parityFeeContext(0, false)

	local, localErr := NewDynamicFeeChecker(&params)(ctx, tx)
	upstreamFee, upstreamPriority, upstreamErr := cosmosevmante.NewDynamicFeeChecker(&params)(ctx, tx)
	require.NoError(t, localErr)
	require.NoError(t, upstreamErr)
	require.Equal(t, upstreamFee, local.EffectiveFee)
	require.Equal(t, upstreamPriority, local.Priority)
	require.Equal(t, fee, local.BaseComponent)
	require.Empty(t, local.TipComponent)
	require.Equal(t, int64(3), local.Priority)
}
