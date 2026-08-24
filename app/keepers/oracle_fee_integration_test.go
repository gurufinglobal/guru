package keepers

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
	initialMinGasPrice, ok := sdkmath.NewIntFromString(constitutiontypes.MinGasPriceScaleFactor)
	require.True(t, ok)
	feeMarketParams := feemarkettypes.DefaultParams()
	feeMarketParams.NoBaseFee = true
	feeMarketParams.BaseFee = sdkmath.LegacyZeroDec()
	feeMarketParams.MinGasPrice = initialMinGasPrice.ToLegacyDec()
	require.NoError(t, feeMarketParams.Validate())
	require.NoError(t, feeMarketKeeper.SetParams(ctx, feeMarketParams))

	initialParams := feeMarketKeeper.GetParams(ctx)
	require.True(t, initialParams.NoBaseFee)
	require.True(t, initialParams.BaseFee.IsZero())
	require.Equal(t, "630000000000.000000000000000000", initialParams.MinGasPrice.String())
	require.Equal(t, feeMarketParams, initialParams)

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
		SubmissionInterval: 2,
	}))
	task, err := oracleKeeper.GetTask(ctx, config.MinGasPriceOracleSymbol)
	require.NoError(t, err)
	require.Equal(t, config.MinGasPriceOracleSymbol, task.GetSymbol())
	require.Equal(t, oracletypes.ValueType_VALUE_TYPE_NUMERIC, task.GetValueType())
	require.True(t, task.GetEnabled())
	require.Equal(t, uint32(2), task.GetSubmissionInterval())

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
	require.Equal(t, int64(20), latest.GetBlockHeight())

	schedule, err := constitutionKeeper.GetMinGasPriceSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(22), schedule.GetEffectiveHeight())
	require.Equal(t, config.MinGasPriceOracleSymbol, schedule.GetSourceSymbol())
	require.Equal(t, "0.5", schedule.GetSourceValue())
	require.Equal(t, int64(20), schedule.GetSourceOracleHeight())
	require.Equal(t, uint32(2), schedule.GetSourceSubmissionIntervalBlocks())
	require.Equal(t, uint32(2), schedule.GetPendingDelayBlocks())
	require.Equal(t, "1260000000000", schedule.GetRawMinGasPrice())
	require.Equal(t, "693000000000.000000000000000000", schedule.GetScheduledMinGasPrice())
	require.Equal(t, "630000000000.000000000000000000", schedule.GetPreviousMinGasPrice())
	require.Equal(t, feeMarketParams, feeMarketKeeper.GetParams(ctx))

	// EndBlock at height 20 prepares height 21, so the height-22 policy remains pending.
	require.NoError(t, constitutionKeeper.ApplyDueMinGasPriceSchedule(ctx))
	require.Equal(t, feeMarketParams, feeMarketKeeper.GetParams(ctx))
	pendingSchedule, err := constitutionKeeper.GetMinGasPriceSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, schedule, pendingSchedule)

	// EndBlock at height 21 applies the policy for height 22 and consumes the schedule.
	dueCtx := ctx.WithBlockHeight(21).WithEventManager(sdk.NewEventManager())
	require.NoError(t, constitutionKeeper.ApplyDueMinGasPriceSchedule(dueCtx))
	expectedParams := feeMarketParams
	expectedParams.MinGasPrice = sdkmath.LegacyMustNewDecFromStr("693000000000")
	require.Equal(t, expectedParams, feeMarketKeeper.GetParams(dueCtx))
	_, err = constitutionKeeper.GetMinGasPriceSchedule(dueCtx)
	require.ErrorIs(t, err, collections.ErrNotFound)
}
