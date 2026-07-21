package types

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSwapProtectionMemoNoIntent(t *testing.T) {
	tests := []struct {
		name string
		memo string
	}{
		{name: "empty memo", memo: ""},
		{name: "plain text memo", memo: "Station exchange"},
		{name: "other JSON namespace", memo: `{"forward":{"receiver":"receiver"}}`},
		{name: "malformed unrelated JSON is opaque", memo: `{"forward":`},
		{name: "transwap in JSON value is not intent", memo: `{"note":"transwap"}`},
		{name: "similar unreserved stem is ordinary memo", memo: `guru.transwap.protection-v1:{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSwapProtectionMemo(tt.memo)
			require.NoError(t, err)
			require.Equal(t, SwapProtectionNoIntent, result.State)
			require.False(t, result.Protection.HasMinAmountOut)
			require.False(t, result.Protection.HasExpectedExchangeRevision)
		})
	}
}

func TestParseSwapProtectionMemoValidIntent(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		hasMinimum       bool
		minimum          string
		hasRevision      bool
		expectedRevision uint64
	}{
		{
			name:       "minimum only",
			body:       `{"min_amount_out":"1000"}`,
			hasMinimum: true,
			minimum:    "1000",
		},
		{
			name:             "revision only",
			body:             `{"expected_exchange_revision":"12"}`,
			hasRevision:      true,
			expectedRevision: 12,
		},
		{
			name:             "both fields",
			body:             `{"min_amount_out":"1000","expected_exchange_revision":"12"}`,
			hasMinimum:       true,
			minimum:          "1000",
			hasRevision:      true,
			expectedRevision: 12,
		},
		{
			name:       "single object with trailing JSON whitespace",
			body:       "{\"min_amount_out\":\"1\"}\n\t",
			hasMinimum: true,
			minimum:    "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memo := SwapProtectionMemoMarkerV1 + tt.body
			result, err := ParseSwapProtectionMemo(memo)
			require.NoError(t, err)
			require.Equal(t, SwapProtectionValidIntent, result.State)
			require.Equal(t, tt.hasMinimum, result.Protection.HasMinAmountOut)
			require.Equal(t, tt.hasRevision, result.Protection.HasExpectedExchangeRevision)
			if tt.hasMinimum {
				require.Equal(t, tt.minimum, result.Protection.MinAmountOut.String())
			}
			if tt.hasRevision {
				require.Equal(t, tt.expectedRevision, result.Protection.ExpectedExchangeRevision)
			}

			// The compatibility wrapper must preserve the same protection values.
			protection, err := ParseSwapProtection(memo)
			require.NoError(t, err)
			require.Equal(t, result.Protection, protection)
		})
	}
}

func TestParseSwapProtectionMemoRejectsMalformedMarkerAndBody(t *testing.T) {
	overflowUint256 := new(big.Int).Lsh(big.NewInt(1), 256).String()
	validMemo := SwapProtectionMemoMarkerV1 + `{"min_amount_out":"1"}`
	tests := []struct {
		name string
		memo string
	}{
		{name: "UTF-8 BOM before marker", memo: "\xEF\xBB\xBF" + validMemo},
		{name: "NUL before marker", memo: "\x00" + validMemo},
		{name: "arbitrary prefix before marker", memo: "x" + validMemo},
		{name: "space before marker", memo: " " + validMemo},
		{name: "stem embedded in plain memo", memo: "note " + SwapProtectionMemoStem + "reference"},
		{name: "unsupported version", memo: SwapProtectionMemoStem + `v2:{"min_amount_out":"1"}`},
		{name: "missing version separator", memo: SwapProtectionMemoStem + `v1{"min_amount_out":"1"}`},
		{name: "marker without body", memo: SwapProtectionMemoMarkerV1},
		{name: "whitespace before body", memo: SwapProtectionMemoMarkerV1 + ` {"min_amount_out":"1"}`},
		{name: "null body", memo: SwapProtectionMemoMarkerV1 + `null`},
		{name: "array body", memo: SwapProtectionMemoMarkerV1 + `[]`},
		{name: "invalid UTF-8 body", memo: SwapProtectionMemoMarkerV1 + "{\"min_amount_out\":\"\xff\"}"},
		{name: "malformed JSON object", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":`},
		{name: "empty protection object", memo: SwapProtectionMemoMarkerV1 + `{}`},
		{name: "duplicate minimum", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":"1","min_amount_out":"2"}`},
		{name: "escaped duplicate minimum", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":"1","min_\u0061mount_out":"2"}`},
		{name: "duplicate revision", memo: SwapProtectionMemoMarkerV1 + `{"expected_exchange_revision":"1","expected_exchange_revision":"2"}`},
		{name: "unknown protection field", memo: SwapProtectionMemoMarkerV1 + `{"min_output_amount":"1"}`},
		{name: "nested legacy schema", memo: SwapProtectionMemoMarkerV1 + `{"transwap":{"min_amount_out":"1"}}`},
		{name: "numeric minimum", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":1}`},
		{name: "null minimum", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":null}`},
		{name: "zero minimum", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":"0"}`},
		{name: "non-canonical minimum", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":"01"}`},
		{name: "minimum uint256 overflow", memo: SwapProtectionMemoMarkerV1 + fmt.Sprintf(`{"min_amount_out":"%s"}`, overflowUint256)},
		{name: "numeric revision", memo: SwapProtectionMemoMarkerV1 + `{"expected_exchange_revision":1}`},
		{name: "zero revision", memo: SwapProtectionMemoMarkerV1 + `{"expected_exchange_revision":"0"}`},
		{name: "non-canonical revision", memo: SwapProtectionMemoMarkerV1 + `{"expected_exchange_revision":"01"}`},
		{name: "revision overflow", memo: SwapProtectionMemoMarkerV1 + fmt.Sprintf(`{"expected_exchange_revision":"%d0"}`, uint64(math.MaxUint64))},
		{name: "trailing object", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":"1"}{}`},
		{name: "trailing scalar", memo: SwapProtectionMemoMarkerV1 + `{"min_amount_out":"1"}true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSwapProtectionMemo(tt.memo)
			require.ErrorIs(t, err, ErrInvalidSwapProtection)
			require.Equal(t, SwapProtectionMalformedIntent, result.State)
			require.False(t, result.Protection.HasMinAmountOut)
			require.False(t, result.Protection.HasExpectedExchangeRevision)

			_, wrapperErr := ParseSwapProtection(tt.memo)
			require.ErrorIs(t, wrapperErr, ErrInvalidSwapProtection)
		})
	}
}

func TestParseSwapProtectionMemoRejectsDeprecatedNamespace(t *testing.T) {
	tests := []string{
		`{"transwap":{"min_amount_out":"1"}}`,
		`{"\u0074ranswap":{"min_amount_out":"1"}}`,
		` {"transwap":{}} `,
		`{"forward":{},"transwap":{"expected_exchange_revision":"1"}}`,
		`{"transwap":{},"transwap":{"min_amount_out":"1"}}`,
	}

	for _, memo := range tests {
		result, err := ParseSwapProtectionMemo(memo)
		require.ErrorIs(t, err, ErrInvalidSwapProtection)
		require.Equal(t, SwapProtectionMalformedIntent, result.State)
	}
}

func TestParseSwapProtectionMemoErrorPrecedenceIsDeterministic(t *testing.T) {
	memo := SwapProtectionMemoMarkerV1 + `{"z_unknown":"1","a_unknown":"1","min_amount_out":0,"expected_exchange_revision":0}`

	for range 100 {
		_, err := ParseSwapProtectionMemo(memo)
		require.ErrorIs(t, err, ErrInvalidSwapProtection)
		require.Contains(t, err.Error(), `unknown transwap protection field "a_unknown"`)
	}

	memo = SwapProtectionMemoMarkerV1 + `{"min_amount_out":0,"expected_exchange_revision":0}`
	for range 100 {
		_, err := ParseSwapProtectionMemo(memo)
		require.ErrorIs(t, err, ErrInvalidSwapProtection)
		require.Contains(t, err.Error(), "min_amount_out must be a JSON string")
	}
}
