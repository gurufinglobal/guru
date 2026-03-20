package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"
)

func TestParseOracleDecimal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "int", in: "123", want: "123"},
		{name: "negative_decimal", in: "-123.456", want: "-123.456"},
		{name: "leading_plus", in: "+1.2300", want: "1.23"},
		{name: "leading_dot", in: ".5", want: "0.5"},
		{name: "trailing_dot", in: "5.", want: "5"},
		{
			name: "precision_truncated",
			in:   "0.107439747239410490763297",
			want: "0.10743974723941049",
		},
		{
			name: "precision_truncated_negative",
			in:   "-0.107439747239410490763297",
			want: "-0.10743974723941049",
		},
		{name: "reject_exponent", in: "1e-3", wantErr: true},
		{name: "reject_fraction", in: "1/3", wantErr: true},
		{name: "reject_double_sign", in: "+-1", wantErr: true},
		{name: "reject_empty", in: "", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseOracleDecimal(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if formatted := FormatOracleDecimal(got); formatted != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, formatted)
			}
		})
	}
}

func TestFormatOracleDecimal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   sdkmath.LegacyDec
		want string
	}{
		{name: "trailing_zero_trim", in: sdkmath.LegacyMustNewDecFromStr("2.500000000000000000"), want: "2.5"},
		{name: "integer_trim", in: sdkmath.LegacyMustNewDecFromStr("5.000000000000000000"), want: "5"},
		{name: "zero", in: sdkmath.LegacyZeroDec(), want: "0"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := FormatOracleDecimal(tc.in); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
