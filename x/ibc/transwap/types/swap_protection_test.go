package types

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSwapProtectionAppliesFieldsIndependently(t *testing.T) {
	tests := []struct {
		name             string
		memo             string
		hasMinimum       bool
		minimum          string
		hasRevision      bool
		expectedRevision uint64
	}{
		{name: "empty memo", memo: ""},
		{name: "plain text memo", memo: "Station exchange"},
		{name: "other JSON namespace", memo: `{"forward":{"receiver":"receiver"}}`},
		{name: "empty transwap namespace", memo: `{"transwap":{}}`},
		{name: "minimum only", memo: `{"transwap":{"min_amount_out":"1000"}}`, hasMinimum: true, minimum: "1000"},
		{name: "revision only", memo: `{"transwap":{"expected_exchange_revision":"12"}}`, hasRevision: true, expectedRevision: 12},
		{
			name:             "both fields with another namespace",
			memo:             `{"transwap":{"min_amount_out":"1000","expected_exchange_revision":"12"},"forward":{}}`,
			hasMinimum:       true,
			minimum:          "1000",
			hasRevision:      true,
			expectedRevision: 12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protection, err := ParseSwapProtection(tt.memo)
			require.NoError(t, err)
			require.Equal(t, tt.hasMinimum, protection.HasMinAmountOut)
			require.Equal(t, tt.hasRevision, protection.HasExpectedExchangeRevision)
			if tt.hasMinimum {
				require.Equal(t, tt.minimum, protection.MinAmountOut.String())
			}
			if tt.hasRevision {
				require.Equal(t, tt.expectedRevision, protection.ExpectedExchangeRevision)
			}
		})
	}
}

func TestParseSwapProtectionRejectsMalformedProtection(t *testing.T) {
	overflowUint256 := new(big.Int).Lsh(big.NewInt(1), 256).String()
	tests := []struct {
		name        string
		memo        string
		expectedErr error
	}{
		{name: "malformed JSON object", memo: `{"transwap":`, expectedErr: ErrInvalidMemo},
		{name: "null namespace", memo: `{"transwap":null}`, expectedErr: ErrInvalidSwapProtection},
		{name: "array namespace", memo: `{"transwap":[]}`, expectedErr: ErrInvalidSwapProtection},
		{name: "duplicate namespace", memo: `{"transwap":{"min_amount_out":"1"},"transwap":{}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "duplicate minimum", memo: `{"transwap":{"min_amount_out":"1","min_amount_out":"2"}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "duplicate revision", memo: `{"transwap":{"expected_exchange_revision":"1","expected_exchange_revision":"2"}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "unknown protection field", memo: `{"transwap":{"min_output_amount":"1"}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "numeric minimum", memo: `{"transwap":{"min_amount_out":1}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "zero minimum", memo: `{"transwap":{"min_amount_out":"0"}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "non-canonical minimum", memo: `{"transwap":{"min_amount_out":"01"}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "minimum uint256 overflow", memo: fmt.Sprintf(`{"transwap":{"min_amount_out":"%s"}}`, overflowUint256), expectedErr: ErrInvalidSwapProtection},
		{name: "numeric revision", memo: `{"transwap":{"expected_exchange_revision":1}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "zero revision", memo: `{"transwap":{"expected_exchange_revision":"0"}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "non-canonical revision", memo: `{"transwap":{"expected_exchange_revision":"01"}}`, expectedErr: ErrInvalidSwapProtection},
		{name: "revision overflow", memo: fmt.Sprintf(`{"transwap":{"expected_exchange_revision":"%d0"}}`, uint64(math.MaxUint64)), expectedErr: ErrInvalidSwapProtection},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSwapProtection(tt.memo)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestParseSwapProtectionErrorPrecedenceIsDeterministic(t *testing.T) {
	memo := `{"transwap":{"z_unknown":"1","a_unknown":"1","min_amount_out":0,"expected_exchange_revision":0}}`

	for range 100 {
		_, err := ParseSwapProtection(memo)
		require.ErrorIs(t, err, ErrInvalidSwapProtection)
		require.Contains(t, err.Error(), `unknown transwap protection field "a_unknown"`)
	}

	memo = `{"transwap":{"min_amount_out":0,"expected_exchange_revision":0}}`
	for range 100 {
		_, err := ParseSwapProtection(memo)
		require.ErrorIs(t, err, ErrInvalidSwapProtection)
		require.Contains(t, err.Error(), "min_amount_out must be a JSON string")
	}
}
