package keepers

import (
	"errors"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/stretchr/testify/require"
)

func TestFeeMarketAdapterGetMinGasPrice(t *testing.T) {
	paramsStore := &mockFeeMarketParamsStore{params: feemarkettypes.DefaultParams()}
	paramsStore.params.MinGasPrice = sdkmath.LegacyNewDecWithPrec(25, 1)
	adapter := newFeeMarketAdapter(paramsStore)

	minGasPrice := adapter.GetMinGasPrice(sdk.Context{})

	require.True(t, minGasPrice.Equal(sdkmath.LegacyNewDecWithPrec(25, 1)))
}

func TestFeeMarketAdapterSetMinGasPricePreservesOtherParams(t *testing.T) {
	paramsStore := &mockFeeMarketParamsStore{params: feemarkettypes.DefaultParams()}
	original := paramsStore.params
	adapter := newFeeMarketAdapter(paramsStore)
	newMinGasPrice := sdkmath.LegacyNewDecWithPrec(15, 1)

	err := adapter.SetMinGasPrice(sdk.Context{}, newMinGasPrice)

	require.NoError(t, err)
	require.Equal(t, 1, paramsStore.setCalls)
	require.True(t, paramsStore.params.MinGasPrice.Equal(newMinGasPrice))
	require.Equal(t, original.NoBaseFee, paramsStore.params.NoBaseFee)
	require.Equal(t, original.BaseFeeChangeDenominator, paramsStore.params.BaseFeeChangeDenominator)
	require.Equal(t, original.ElasticityMultiplier, paramsStore.params.ElasticityMultiplier)
	require.True(t, original.BaseFee.Equal(paramsStore.params.BaseFee))
	require.Equal(t, original.EnableHeight, paramsStore.params.EnableHeight)
	require.True(t, original.MinGasMultiplier.Equal(paramsStore.params.MinGasMultiplier))
}

func TestFeeMarketAdapterSetMinGasPriceRejectsInvalidParams(t *testing.T) {
	paramsStore := &mockFeeMarketParamsStore{params: feemarkettypes.DefaultParams()}
	adapter := newFeeMarketAdapter(paramsStore)

	err := adapter.SetMinGasPrice(
		sdk.Context{},
		sdkmath.LegacyNewDec(-1),
	)

	require.ErrorContains(t, err, "validate feemarket params")
	require.Zero(t, paramsStore.setCalls)
}

func TestFeeMarketAdapterSetMinGasPricePropagatesStoreError(t *testing.T) {
	expectedErr := errors.New("write failed")
	paramsStore := &mockFeeMarketParamsStore{
		params: feemarkettypes.DefaultParams(),
		setErr: expectedErr,
	}
	adapter := newFeeMarketAdapter(paramsStore)

	err := adapter.SetMinGasPrice(
		sdk.Context{},
		sdkmath.LegacyOneDec(),
	)

	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, 1, paramsStore.setCalls)
}

type mockFeeMarketParamsStore struct {
	params   feemarkettypes.Params
	setErr   error
	setCalls int
}

func (m *mockFeeMarketParamsStore) GetParams(sdk.Context) feemarkettypes.Params {
	return m.params
}

func (m *mockFeeMarketParamsStore) SetParams(
	_ sdk.Context,
	params feemarkettypes.Params,
) error {
	m.setCalls++
	if m.setErr != nil {
		return m.setErr
	}
	m.params = params
	return nil
}
