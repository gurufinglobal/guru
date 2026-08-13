package domain

import (
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
)

func TestNormalizeDecimal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"1", "1.000000000000000000", true},
		{"+1.2300e2", "123.000000000000000000", true},
		{".5", "0.500000000000000000", true},
		{"1e-18", "0.000000000000000001", true},
		{"10e-19", "0.000000000000000001", true},
		{"-0e256", "0.000000000000000000", true},
		{"1e-19", "", false},
		{"1.0000000000000000001", "", false},
		{"1e257", "", false},
		{"NaN", "", false},
		{" 1", "", false},
		{"1_000", "", false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeDecimal(test.input)
			if test.ok && err != nil {
				t.Fatalf("NormalizeDecimal() error = %v", err)
			}
			if !test.ok && err == nil {
				t.Fatalf("NormalizeDecimal() unexpectedly accepted %q as %q", test.input, got)
			}
			if got != test.want {
				t.Fatalf("NormalizeDecimal() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMedianUsesOverflowSafeTowardZeroMean(t *testing.T) {
	t.Parallel()
	atto := sdkmath.LegacySmallestDec()
	zero := sdkmath.LegacyZeroDec()
	negativeAtto := atto.Neg()
	tests := []struct {
		name   string
		values []sdkmath.LegacyDec
		want   string
	}{
		{"odd", []sdkmath.LegacyDec{sdkmath.LegacyNewDec(3), sdkmath.LegacyNewDec(1), sdkmath.LegacyNewDec(2)}, "2.000000000000000000"},
		{"positive half atto", []sdkmath.LegacyDec{zero, atto}, "0.000000000000000000"},
		{"negative half atto", []sdkmath.LegacyDec{negativeAtto, zero}, "0.000000000000000000"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Median(test.values)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != test.want {
				t.Fatalf("Median() = %s, want %s", got, test.want)
			}
		})
	}

	atoms := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
	atoms.Mul(atoms, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	atoms.Sub(atoms, big.NewInt(1))
	maximum := sdkmath.LegacyNewDecFromBigIntWithPrec(atoms, sdkmath.LegacyPrecision)
	got, err := Median([]sdkmath.LegacyDec{maximum, maximum})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(maximum) {
		t.Fatalf("maximum median changed: got %s want %s", got, maximum)
	}
}

func TestNormalizeSymbolRejectsInvalidUTF8BeforeUppercase(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeSymbol(string([]byte{0xff, 'a'})); err == nil {
		t.Fatal("NormalizeSymbol accepted invalid UTF-8")
	}
	got, err := NormalizeSymbol(" btc/usd ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "BTC/USD" {
		t.Fatalf("NormalizeSymbol() = %q", got)
	}
}

func FuzzNormalizeDecimal(f *testing.F) {
	for _, seed := range []string{"1", "1e-18", "-0.5", "1e999", "NaN", "1.0000000000000000001"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		normalized, err := NormalizeDecimal(input)
		if err != nil {
			return
		}
		decimal, err := ParseCanonicalDecimal(normalized)
		if err != nil {
			t.Fatalf("accepted value is not canonical: %q: %v", normalized, err)
		}
		if decimal.String() != normalized {
			t.Fatalf("canonical value changed: %q -> %q", normalized, decimal.String())
		}
	})
}
