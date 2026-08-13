package app

import (
	"bytes"
	"testing"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/config"
	constitutionkeeper "github.com/gurufinglobal/guru/v2/x/constitution/keeper"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	oraclekeeper "github.com/gurufinglobal/guru/v2/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func TestOracleConstitutionFeeMarketIntegration(t *testing.T) {
	feeMarketKey := storetypes.NewKVStoreKey(feemarkettypes.StoreKey)
	feeMarketTransientKey := storetypes.NewTransientStoreKey(feemarkettypes.TransientKey)
	constitutionKey := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	oracleKey := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			feemarkettypes.StoreKey:    feeMarketKey,
			constitutiontypes.StoreKey: constitutionKey,
			oracletypes.StoreKey:       oracleKey,
		},
		map[string]*storetypes.TransientStoreKey{
			feemarkettypes.TransientKey: feeMarketTransientKey,
		},
		nil,
	).WithBlockHeight(20).WithEventManager(sdk.NewEventManager())

	registry := codectypes.NewInterfaceRegistry()
	constitutiontypes.RegisterInterfaces(registry)
	oracletypes.RegisterInterfaces(registry)
	appCodec := codec.NewProtoCodec(registry)
	authority := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	accountCodec := evmaddress.NewEvmCodec(config.Bech32PrefixAccAddr)

	feeMarketKeeper := feemarketkeeper.NewKeeper(
		appCodec,
		authority,
		feeMarketKey,
		feeMarketTransientKey,
	)
	feeMarketParams := feemarkettypes.DefaultParams()
	feeMarketParams.NoBaseFee = true
	feeMarketParams.BaseFee = sdkmath.LegacyZeroDec()
	feeMarketParams.MinGasPrice = mustInt(constitutiontypes.MinGasPriceScaleFactor).ToLegacyDec()
	require.NoError(t, feeMarketParams.Validate())
	require.NoError(t, feeMarketKeeper.SetParams(ctx, feeMarketParams))

	constitutionKeeper := constitutionkeeper.NewKeeper(
		authority,
		runtime.NewKVStoreService(constitutionKey),
		appCodec,
		accountCodec,
		nil,
	)
	constitutionKeeper.SetFeeMarketKeeper(newFeeMarketAdapter(feeMarketKeeper))
	oracleKeeper := oraclekeeper.NewKeeper(
		runtime.NewKVStoreService(oracleKey),
		appCodec,
		accountCodec,
		&constitutionKeeper,
	)
	oracleKeeper.SetHooks(oracletypes.NewMultiOracleHooks(&constitutionKeeper))

	require.NoError(t, oracleKeeper.SetParams(ctx, oraclekeeper.DefaultParams()))
	require.NoError(t, oracleKeeper.SetTaskDefinition(ctx, &oracletypes.OracleTask{
		Symbol:             config.MinGasPriceOracleSymbol,
		ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}))
	require.NoError(t, oracleKeeper.ApplyOracleValues(ctx, []*oracletypes.OracleValue{{
		Symbol:        config.MinGasPriceOracleSymbol,
		ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Value:         "0.5",
		BlockHeight:   20,
		BlockTimeUnix: 1_700_000_000,
	}}))

	latest, err := oracleKeeper.GetLatestValue(ctx, config.MinGasPriceOracleSymbol)
	require.NoError(t, err)
	require.Equal(t, "0.5", latest.GetValue())
	schedule, err := constitutionKeeper.GetMinGasPriceSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(21), schedule.GetEffectiveHeight())
	require.Equal(t, "1260000000000", schedule.GetRawMinGasPrice())
	require.True(t, sdkmath.LegacyMustNewDecFromStr(schedule.GetPreviousMinGasPrice()).Equal(
		feeMarketParams.MinGasPrice,
	))
	expectedMinGasPrice := sdkmath.LegacyMustNewDecFromStr("693000000000")
	require.True(t, sdkmath.LegacyMustNewDecFromStr(schedule.GetScheduledMinGasPrice()).Equal(
		expectedMinGasPrice,
	))
	require.True(t, feeMarketKeeper.GetParams(ctx).MinGasPrice.Equal(feeMarketParams.MinGasPrice))

	require.NoError(t, constitutionKeeper.ApplyDueMinGasPriceSchedule(ctx))
	expectedParams := feeMarketParams
	expectedParams.MinGasPrice = expectedMinGasPrice
	require.Equal(t, expectedParams, feeMarketKeeper.GetParams(ctx))
	_, err = constitutionKeeper.GetMinGasPriceSchedule(ctx)
	require.ErrorIs(t, err, collections.ErrNotFound)
}
