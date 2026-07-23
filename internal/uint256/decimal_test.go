package uint256

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCanonical(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "zero", raw: "0", want: "0"},
		{name: "one", raw: "1", want: "1"},
		{name: "decimal ten", raw: "10", want: "10"},
		{name: "maximum", raw: MaxDecimalString, want: MaxDecimalString},
		{name: "empty", raw: "", wantErr: true},
		{name: "leading zero", raw: "010", wantErr: true},
		{name: "double zero", raw: "00", wantErr: true},
		{name: "plus sign", raw: "+10", wantErr: true},
		{name: "minus sign", raw: "-10", wantErr: true},
		{name: "leading space", raw: " 10", wantErr: true},
		{name: "trailing space", raw: "10 ", wantErr: true},
		{name: "hex prefix", raw: "0x10", wantErr: true},
		{name: "octal prefix", raw: "0o10", wantErr: true},
		{name: "separator", raw: "1_0", wantErr: true},
		{name: "decimal point", raw: "10.0", wantErr: true},
		{name: "exponent", raw: "1e1", wantErr: true},
		{name: "unicode digits", raw: "１０", wantErr: true},
		{name: "overflow", raw: "115792089237316195423570985008687907853269984665640564039457584007913129639936", wantErr: true},
		{name: "oversized input", raw: "999999999999999999999999999999999999999999999999999999999999999999999999999999", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCanonical(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}
}

func TestParseCanonicalPositive(t *testing.T) {
	zero, err := ParseCanonical("0")
	require.NoError(t, err)
	require.True(t, zero.IsZero())

	_, err = ParseCanonicalPositive("0")
	require.Error(t, err)

	one, err := ParseCanonicalPositive("1")
	require.NoError(t, err)
	require.Equal(t, "1", one.String())
	require.Equal(t, MaxDecimalString, Max().String())
}
