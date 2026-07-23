package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"cosmossdk.io/core/address"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
)

type keeperTestFixture struct {
	ctx              sdk.Context
	keeper           Keeper
	bankKeeper       *mockBankKeeper
	feeMarketKeeper  *mockFeeMarketKeeper
	authority        string
	moderatorAddress string
	baseAddress      string
}

func setupKeeperFixture(t *testing.T) keeperTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_constitution_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	authorityBytes := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	authorityAddress := testAddress(t, accountCodec, 0x01)
	moderatorAddress := testAddress(t, accountCodec, 0x02)
	baseAddress := testAddress(t, accountCodec, 0x03)
	bankKeeper := newMockBankKeeper()
	feeMarketKeeper := newMockFeeMarketKeeper()

	keeper := NewKeeper(authorityBytes, runtime.NewKVStoreService(key), codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), accountCodec, bankKeeper)
	keeper.SetFeeMarketKeeper(feeMarketKeeper)
	require.NoError(t, keeper.SetParams(testCtx.Ctx, testParams("10")))
	require.NoError(t, keeper.SetBaseAddress(testCtx.Ctx, baseAddress))
	require.NoError(t, keeper.SetModeratorAddress(testCtx.Ctx, moderatorAddress))
	require.NoError(t, keeper.SetSeparationRatio(testCtx.Ctx, testSeparationRatio(200_000, 300_000, 500_000)))

	return keeperTestFixture{
		ctx:              testCtx.Ctx,
		keeper:           keeper,
		bankKeeper:       bankKeeper,
		feeMarketKeeper:  feeMarketKeeper,
		authority:        authorityAddress,
		moderatorAddress: moderatorAddress,
		baseAddress:      baseAddress,
	}
}

func setupKeeperFixtureWithoutParams(t *testing.T) keeperTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_constitution_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	authorityBytes := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	authorityAddress := testAddress(t, accountCodec, 0x01)
	moderatorAddress := testAddress(t, accountCodec, 0x02)
	baseAddress := testAddress(t, accountCodec, 0x03)
	bankKeeper := newMockBankKeeper()
	feeMarketKeeper := newMockFeeMarketKeeper()

	keeper := NewKeeper(authorityBytes, runtime.NewKVStoreService(key), codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), accountCodec, bankKeeper)
	keeper.SetFeeMarketKeeper(feeMarketKeeper)

	return keeperTestFixture{
		ctx:              testCtx.Ctx,
		keeper:           keeper,
		bankKeeper:       bankKeeper,
		feeMarketKeeper:  feeMarketKeeper,
		authority:        authorityAddress,
		moderatorAddress: moderatorAddress,
		baseAddress:      baseAddress,
	}
}

func testParams(amount string) *constitutiontypes.Params {
	amountInt, ok := sdkmath.NewIntFromString(amount)
	if !ok {
		panic("invalid test coin amount: " + amount)
	}

	return &constitutiontypes.Params{
		MinValidatorBondAmount: &sdk.Coin{
			Denom:  appparams.BaseDenom,
			Amount: amountInt,
		},
	}
}

func testSeparationRatio(base, burn, validators uint32) *constitutiontypes.SeparationRatio {
	return &constitutiontypes.SeparationRatio{
		BasePpm:       base,
		BurnPpm:       burn,
		ValidatorsPpm: validators,
	}
}

func testAddress(t *testing.T, accountCodec address.Codec, b byte) string {
	t.Helper()

	address, err := accountCodec.BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}

func testHexAddress(b byte) string {
	return "0x" + hex.EncodeToString(bytes.Repeat([]byte{b}, 20))
}

type mockFeeMarketKeeper struct {
	params feemarkettypes.Params
}

func newMockFeeMarketKeeper() *mockFeeMarketKeeper {
	params := feemarkettypes.DefaultParams()
	params.NoBaseFee = true
	params.BaseFee = sdkmath.LegacyZeroDec()
	params.MinGasPrice = mustTestInt(constitutiontypes.MinGasPriceScaleFactor).ToLegacyDec()
	return &mockFeeMarketKeeper{params: params}
}

func (m *mockFeeMarketKeeper) GetParams(_ sdk.Context) feemarkettypes.Params {
	return m.params
}

func (m *mockFeeMarketKeeper) SetParams(_ sdk.Context, params feemarkettypes.Params) error {
	m.params = params
	return nil
}

func mustTestInt(value string) sdkmath.Int {
	parsed, ok := sdkmath.NewIntFromString(value)
	if !ok {
		panic(fmt.Sprintf("invalid test integer %q", value))
	}
	return parsed
}

func mustTestDec(value string) sdkmath.LegacyDec {
	parsed, err := sdkmath.LegacyNewDecFromStr(value)
	if err != nil {
		panic(fmt.Sprintf("invalid test decimal %q: %v", value, err))
	}
	return parsed
}

type mockBankKeeper struct {
	moduleBalances  map[string]sdk.Coins
	accountBalances map[string]sdk.Coins
	blockedAddrs    map[string]bool
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{
		moduleBalances:  make(map[string]sdk.Coins),
		accountBalances: make(map[string]sdk.Coins),
		blockedAddrs:    make(map[string]bool),
	}
}

func (m *mockBankKeeper) SetModuleBalance(moduleName string, balances sdk.Coins) {
	m.moduleBalances[moduleName] = balances.Sort()
}

func (m *mockBankKeeper) GetModuleBalance(moduleName string) sdk.Coins {
	return m.moduleBalances[moduleName]
}

func (m *mockBankKeeper) GetAccountBalance(address sdk.AccAddress) sdk.Coins {
	return m.accountBalances[address.String()]
}

func (m *mockBankKeeper) SetBlockedAddr(address sdk.AccAddress, blocked bool) {
	m.blockedAddrs[string(address)] = blocked
}

func (m *mockBankKeeper) SetBlockedAddressString(address string, blocked bool) {
	m.blockedAddrs[address] = blocked
}

func (m *mockBankKeeper) BlockedAddr(address sdk.AccAddress) bool {
	return m.blockedAddrs[string(address)]
}

func (m *mockBankKeeper) GetBlockedAddresses() map[string]bool {
	return m.blockedAddrs
}

func (m *mockBankKeeper) GetAllBalances(_ context.Context, address sdk.AccAddress) sdk.Coins {
	feeCollectorAddress := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	if address.Equals(feeCollectorAddress) {
		return m.moduleBalances[authtypes.FeeCollectorName]
	}

	return m.accountBalances[address.String()]
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(
	_ context.Context,
	senderModule string,
	recipientAddress sdk.AccAddress,
	amount sdk.Coins,
) error {
	currentBalance := m.moduleBalances[senderModule]
	if !currentBalance.IsAllGTE(amount) {
		return fmt.Errorf("insufficient module funds")
	}

	m.moduleBalances[senderModule] = currentBalance.Sub(amount...)
	recipientKey := recipientAddress.String()
	m.accountBalances[recipientKey] = m.accountBalances[recipientKey].Add(amount...)

	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToModule(
	_ context.Context,
	senderModule, recipientModule string,
	amount sdk.Coins,
) error {
	currentBalance := m.moduleBalances[senderModule]
	if !currentBalance.IsAllGTE(amount) {
		return fmt.Errorf("insufficient module funds")
	}

	m.moduleBalances[senderModule] = currentBalance.Sub(amount...)
	m.moduleBalances[recipientModule] = m.moduleBalances[recipientModule].Add(amount...)

	return nil
}

func (m *mockBankKeeper) BurnCoins(_ context.Context, moduleName string, amount sdk.Coins) error {
	currentBalance := m.moduleBalances[moduleName]
	if !currentBalance.IsAllGTE(amount) {
		return fmt.Errorf("insufficient module funds")
	}

	m.moduleBalances[moduleName] = currentBalance.Sub(amount...)
	return nil
}
