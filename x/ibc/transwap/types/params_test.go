package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateParamsBoundsAutomaticRefundRetryGas(t *testing.T) {
	params := DefaultParams()
	params.MaxRefundRetries = MaximumMaxRefundRetries
	require.NoError(t, ValidateParams(params))

	params.MaxRefundRetries++
	require.ErrorIs(t, ValidateParams(params), ErrInvalidParams)
}
