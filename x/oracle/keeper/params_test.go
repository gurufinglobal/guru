package keeper

import (
	"testing"

	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestValidateParams(t *testing.T) {
	require.Error(t, ValidateParams(nil))
	require.Error(t, ValidateParams(&oraclev1.Params{MinValidators: 0, MinSources: 3, HistoryLimit: 100}))
	require.Error(t, ValidateParams(&oraclev1.Params{MinValidators: 1, MinSources: 0, HistoryLimit: 100}))
	require.Error(t, ValidateParams(&oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 0}))
	require.NoError(t, ValidateParams(DefaultParams()))
}

func TestDefaultParams(t *testing.T) {
	params := DefaultParams()

	require.Equal(t, uint32(1), params.GetMinValidators())
	require.Equal(t, uint32(3), params.GetMinSources())
	require.Equal(t, uint32(100), params.GetHistoryLimit())
}
