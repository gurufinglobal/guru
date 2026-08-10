package ante

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	coreaddress "cosmossdk.io/core/address"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	evmante "github.com/cosmos/evm/ante"
	anteinterfaces "github.com/cosmos/evm/ante/interfaces"
	antetypes "github.com/cosmos/evm/ante/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	storetypes "cosmossdk.io/store/types"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

var (
	errRouterPolicyReached = errors.New("router reached application fee policy")
	routerEVMConfigOnce    sync.Once
)

func TestAnteRouterCosmosRoutesUseApplicationFeePolicy(t *testing.T) {
	configureRouterEVM(t)

	dynamicOption, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{})
	require.NoError(t, err)

	tests := []struct {
		name       string
		extensions []*codectypes.Any
	}{
		{name: "normal Cosmos transaction"},
		{name: "dynamic-fee Cosmos transaction", extensions: []*codectypes.Any{dynamicOption}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options, policy, evmKeeper, feeMarketKeeper := newRouterHandlerOptions()
			handler, err := NewAnteHandler(options)
			require.NoError(t, err)

			tx := routerTestTx{
				msgs:       []sdk.Msg{&banktypes.MsgSend{}},
				gas:        1_000_000,
				payer:      sdk.AccAddress(strings.Repeat("p", 20)),
				extensions: test.extensions,
			}
			_, err = handler(routerContext(false), tx, false)

			require.ErrorIs(t, err, errRouterPolicyReached)
			require.Equal(t, 1, policy.calls)
			expectedPayer, err := evmaddress.NewEvmCodec("guru").BytesToString(tx.payer)
			require.NoError(t, err)
			require.Equal(t, expectedPayer, policy.payer)
			require.Equal(t, tx.msgs, policy.msgs)
			require.Zero(t, evmKeeper.paramsCalls, "Cosmos routes must not enter the EVM mono ante chain")
			require.Equal(t, 1, feeMarketKeeper.paramsCalls)
		})
	}
}

func TestAnteRouterEthereumDelegatesToUpstreamWithoutPolicyResolution(t *testing.T) {
	ethereumOption, err := codectypes.NewAnyWithValue(&evmtypes.ExtensionOptionsEthereumTx{})
	require.NoError(t, err)

	for _, cosmosVirtual := range []bool{false, true} {
		t.Run(fmt.Sprintf("Cosmos virtual=%t", cosmosVirtual), func(t *testing.T) {
			options, policy, evmKeeper, feeMarketKeeper := newRouterHandlerOptions()
			options.CosmosVirtualFeeCollection = cosmosVirtual
			handler, err := NewAnteHandler(options)
			require.NoError(t, err)

			// This deliberately is not a ProtoTxProvider. The important observable is
			// that Guru returns the exact upstream mono-ante error before fee policy is
			// consulted, while still entering the upstream EVM keeper/fee-market path.
			tx := routerTestTx{
				msgs:       []sdk.Msg{&evmtypes.MsgEthereumTx{}},
				gas:        1_000_000,
				extensions: []*codectypes.Any{ethereumOption},
			}
			_, localErr := handler(routerContext(false), tx, false)
			require.Error(t, localErr)
			require.Zero(t, policy.calls)
			require.Equal(t, 1, evmKeeper.paramsCalls)
			require.Equal(t, 1, feeMarketKeeper.paramsCalls)

			_, upstreamErr := evmante.NewAnteHandler(options.EVMOptions)(routerContext(false), tx, false)
			require.EqualError(t, localErr, upstreamErr.Error())
			require.ErrorIs(t, localErr, sdkerrors.ErrUnknownRequest)
		})
	}
}

func TestAnteRouterReadsFeeMarketParamsForEveryCosmosTransaction(t *testing.T) {
	configureRouterEVM(t)

	options, policy, _, feeMarketKeeper := newRouterHandlerOptions()
	handler, err := NewAnteHandler(options)
	require.NoError(t, err)
	tx := routerTestTx{
		msgs:  []sdk.Msg{&banktypes.MsgSend{}},
		gas:   1_000_000,
		payer: sdk.AccAddress(strings.Repeat("p", 20)),
	}

	_, firstErr := handler(routerContext(false), tx, false)
	require.ErrorIs(t, firstErr, errRouterPolicyReached)
	require.Equal(t, 1, feeMarketKeeper.paramsCalls)
	require.Equal(t, 1, policy.calls)

	// Mutating the keeper response after construction must affect the next tx.
	// A captured parameter snapshot would incorrectly reach policy again.
	feeMarketKeeper.params.MinGasPrice = sdkmath.LegacyOneDec()
	_, secondErr := handler(routerContext(false), tx, false)
	require.ErrorIs(t, secondErr, sdkerrors.ErrInsufficientFee)
	require.Equal(t, 2, feeMarketKeeper.paramsCalls)
	require.Equal(t, 1, policy.calls)
}

func TestAnteRouterUnsupportedExtensionPreservesUpstreamV07Error(t *testing.T) {
	options, policy, _, _ := newRouterHandlerOptions()
	handler, err := NewAnteHandler(options)
	require.NoError(t, err)

	tx := routerTestTx{
		gas: 1_000_000,
		extensions: []*codectypes.Any{
			{TypeUrl: "/guru.test.v1.UnsupportedExtension"},
		},
	}
	_, localErr := handler(routerContext(false), tx, false)
	_, upstreamErr := evmante.NewAnteHandler(options.EVMOptions)(routerContext(false), tx, false)

	require.EqualError(t, localErr, upstreamErr.Error())
	require.ErrorIs(t, localErr, sdkerrors.ErrUnknownExtensionOptions)
	require.Zero(t, policy.calls)
}

func TestAnteRouterHandlerOptionsValidation(t *testing.T) {
	t.Run("fee policy dependency", func(t *testing.T) {
		options, _, _, _ := newRouterHandlerOptions()
		options.FeePolicyKeeper = nil
		_, err := NewAnteHandler(options)
		require.ErrorIs(t, err, sdkerrors.ErrLogic)
		require.ErrorContains(t, err, "fee policy keeper is required for AnteHandler")
	})

	t.Run("Cosmos fee bank dependency", func(t *testing.T) {
		options, _, _, _ := newRouterHandlerOptions()
		options.CosmosFeeBankKeeper = nil
		_, err := NewAnteHandler(options)
		require.ErrorIs(t, err, sdkerrors.ErrLogic)
		require.ErrorContains(t, err, "Cosmos fee bank keeper is required for AnteHandler")
	})

	t.Run("virtual Cosmos fee collection capability", func(t *testing.T) {
		options, _, _, _ := newRouterHandlerOptions()
		options.CosmosFeeBankKeeper = &routerBankKeeperWithoutVirtual{}
		_, err := NewAnteHandler(options)
		require.ErrorIs(t, err, sdkerrors.ErrLogic)
		require.ErrorContains(t, err, "bank keeper must support virtual Cosmos fee collection")
	})

	t.Run("normal Cosmos collection does not require virtual capability", func(t *testing.T) {
		options, _, _, _ := newRouterHandlerOptions()
		options.CosmosVirtualFeeCollection = false
		options.CosmosFeeBankKeeper = &routerBankKeeperWithoutVirtual{}
		_, err := NewAnteHandler(options)
		require.NoError(t, err)
	})
}

func TestSelectCosmosFeeCollectorSnapshotsIndependentMode(t *testing.T) {
	payer := sdk.AccAddress(strings.Repeat("p", 20))
	fee := sdk.NewCoins(sdk.NewInt64Coin("aguru", 7))

	t.Run("normal", func(t *testing.T) {
		keeper := &routerBankKeeper{}
		collector, err := selectCosmosFeeCollector(keeper, false)
		require.NoError(t, err)
		require.NoError(t, collector(context.Background(), payer, authtypes.FeeCollectorName, fee))
		require.Equal(t, 1, keeper.normalCalls)
		require.Zero(t, keeper.virtualCalls)
	})

	t.Run("virtual snapshot", func(t *testing.T) {
		keeper := &routerBankKeeper{}
		virtualEnabled := true
		collector, err := selectCosmosFeeCollector(keeper, virtualEnabled)
		require.NoError(t, err)

		// Changing the source option after construction cannot alter the selected
		// transaction path.
		virtualEnabled = false
		require.False(t, virtualEnabled)
		require.NoError(t, collector(context.Background(), payer, authtypes.FeeCollectorName, fee))
		require.Zero(t, keeper.normalCalls)
		require.Equal(t, 1, keeper.virtualCalls)
	})
}

func configureRouterEVM(t *testing.T) {
	t.Helper()
	routerEVMConfigOnce.Do(func() {
		require.NoError(t, evmtypes.NewEVMConfigurator().
			WithEVMCoinInfo(evmtypes.EvmCoinInfo{
				Denom:         "arouter",
				ExtendedDenom: "arouter",
				DisplayDenom:  "router",
				Decimals:      18,
			}).
			Configure())
	})
}

func newRouterHandlerOptions() (HandlerOptions, *routerPolicyKeeper, *routerEVMKeeper, *routerFeeMarketKeeper) {
	policy := &routerPolicyKeeper{err: errRouterPolicyReached}
	evmKeeper := &routerEVMKeeper{params: evmtypes.DefaultParams()}
	feeMarketParams := feemarkettypes.DefaultParams()
	feeMarketParams.MinGasPrice = sdkmath.LegacyZeroDec()
	feeMarketKeeper := &routerFeeMarketKeeper{params: feeMarketParams}
	accountKeeper := &routerAccountKeeper{
		params:     authtypes.DefaultParams(),
		address:    evmaddress.NewEvmCodec("guru"),
		moduleAddr: sdk.AccAddress(strings.Repeat("f", 20)),
	}

	return HandlerOptions{
		EVMOptions: evmante.HandlerOptions{
			Cdc:                    codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
			AccountKeeper:          accountKeeper,
			BankKeeper:             &routerBankKeeper{},
			IBCKeeper:              new(ibckeeper.Keeper),
			FeeMarketKeeper:        feeMarketKeeper,
			EvmKeeper:              evmKeeper,
			ExtensionOptionChecker: antetypes.HasDynamicFeeExtensionOption,
			SignModeHandler:        new(txsigning.HandlerMap),
			SigGasConsumer: func(storetypes.GasMeter, signing.SignatureV2, authtypes.Params) error {
				return nil
			},
			PendingTxListener: func(common.Hash) {},
		},
		CosmosFeeBankKeeper:        &routerBankKeeper{},
		CosmosVirtualFeeCollection: true,
		FeePolicyKeeper:            policy,
	}, policy, evmKeeper, feeMarketKeeper
}

type routerPolicyKeeper struct {
	err   error
	calls int
	payer string
	msgs  []sdk.Msg
}

func (keeper *routerPolicyKeeper) ResolveDiscount(
	_ context.Context,
	payer string,
	msgs []sdk.Msg,
) (feepolicytypes.Discount, error) {
	keeper.calls++
	keeper.payer = payer
	keeper.msgs = msgs
	return feepolicytypes.Discount{}, keeper.err
}

type routerAccountKeeper struct {
	anteinterfaces.AccountKeeper
	params     authtypes.Params
	address    coreaddress.Codec
	moduleAddr sdk.AccAddress
}

func (keeper *routerAccountKeeper) GetParams(context.Context) authtypes.Params {
	return keeper.params
}

func (keeper *routerAccountKeeper) GetModuleAddress(string) sdk.AccAddress {
	return keeper.moduleAddr
}

func (keeper *routerAccountKeeper) AddressCodec() coreaddress.Codec {
	return keeper.address
}

type routerBankKeeper struct {
	anteinterfaces.BankKeeper
	normalCalls  int
	virtualCalls int
}

func (keeper *routerBankKeeper) SendCoinsFromAccountToModule(
	context.Context,
	sdk.AccAddress,
	string,
	sdk.Coins,
) error {
	keeper.normalCalls++
	return nil
}

func (keeper *routerBankKeeper) SendCoinsFromAccountToModuleVirtual(
	context.Context,
	sdk.AccAddress,
	string,
	sdk.Coins,
) error {
	keeper.virtualCalls++
	return nil
}

type routerBankKeeperWithoutVirtual struct {
	anteinterfaces.BankKeeper
}

func (*routerBankKeeperWithoutVirtual) SendCoinsFromAccountToModule(
	context.Context,
	sdk.AccAddress,
	string,
	sdk.Coins,
) error {
	return nil
}

type routerEVMKeeper struct {
	anteinterfaces.EVMKeeper
	params      evmtypes.Params
	paramsCalls int
}

func (keeper *routerEVMKeeper) GetParams(sdk.Context) evmtypes.Params {
	keeper.paramsCalls++
	return keeper.params
}

type routerFeeMarketKeeper struct {
	params      feemarkettypes.Params
	paramsCalls int
}

func (keeper *routerFeeMarketKeeper) GetParams(sdk.Context) feemarkettypes.Params {
	keeper.paramsCalls++
	return keeper.params
}

func (*routerFeeMarketKeeper) AddTransientGasWanted(sdk.Context, uint64) (uint64, error) {
	return 0, nil
}

type routerTestTx struct {
	msgs       []sdk.Msg
	fee        sdk.Coins
	gas        uint64
	payer      sdk.AccAddress
	granter    sdk.AccAddress
	extensions []*codectypes.Any
}

var (
	_ sdk.FeeTx                   = routerTestTx{}
	_ authsigning.SigVerifiableTx = routerTestTx{}
)

func (tx routerTestTx) GetMsgs() []sdk.Msg { return tx.msgs }

func (routerTestTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx routerTestTx) GetGas() uint64 { return tx.gas }

func (tx routerTestTx) GetFee() sdk.Coins { return tx.fee }

func (tx routerTestTx) FeePayer() []byte { return tx.payer }

func (tx routerTestTx) FeeGranter() []byte { return tx.granter }

func (tx routerTestTx) GetExtensionOptions() []*codectypes.Any { return tx.extensions }

func (routerTestTx) GetNonCriticalExtensionOptions() []*codectypes.Any { return nil }

func (routerTestTx) GetMemo() string { return "" }

func (routerTestTx) GetTimeoutHeight() uint64 { return 0 }

func (routerTestTx) GetTimeoutTimeStamp() time.Time { return time.Time{} }

func (routerTestTx) GetSigners() ([][]byte, error) { return nil, nil }

func (routerTestTx) GetPubKeys() ([]cryptotypes.PubKey, error) { return nil, nil }

func (routerTestTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }

func routerContext(checkTx bool) sdk.Context {
	return sdk.NewContext(
		nil,
		cmtproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0)},
		checkTx,
		log.NewNopLogger(),
	)
}
