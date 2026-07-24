package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

const testMsgType = "/cosmos.bank.v1beta1.MsgSend"

func TestValidateFeeDiscount(t *testing.T) {
	rule := func(kind, msg, amount string) Discount {
		var value sdkmath.LegacyDec
		if amount != "" {
			value = sdkmath.LegacyMustNewDecFromStr(amount)
		}
		return Discount{DiscountType: kind, MsgType: msg, Amount: value}
	}
	for _, tc := range []struct {
		name     string
		discount Discount
		valid    bool
	}{
		{"fractional percent", rule(FeeDiscountTypePercent, testMsgType, "12.5"), true},
		{"percent upper bound", rule(FeeDiscountTypePercent, testMsgType, "100"), true},
		{"fixed above one hundred", rule(FeeDiscountTypeFixed, testMsgType, "1000000.25"), true},
		{"unset amount", rule(FeeDiscountTypePercent, testMsgType, ""), false},
		{"zero amount", rule(FeeDiscountTypePercent, testMsgType, "0"), false},
		{"percent above upper bound", rule(FeeDiscountTypePercent, testMsgType, "100.000000000000000001"), false},
		{"unknown type", rule("Percent", testMsgType, "10"), false},
		{"empty message type", rule(FeeDiscountTypePercent, "", "10"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFeeDiscount(tc.discount)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestValidateAccountDiscountStructure(t *testing.T) {
	rule := Discount{
		DiscountType: FeeDiscountTypeFixed,
		MsgType:      testMsgType,
		Amount:       sdkmath.LegacyMustNewDecFromStr("0.25"),
	}
	module := func(name string, rules ...Discount) ModuleDiscount {
		return ModuleDiscount{Module: name, Discounts: rules}
	}
	for _, tc := range []struct {
		name   string
		policy AccountDiscount
		valid  bool
	}{
		{"global policy", AccountDiscount{Modules: []ModuleDiscount{module("bank", rule)}}, true},
		{"empty module", AccountDiscount{Modules: []ModuleDiscount{module("", rule)}}, false},
		{"duplicate module", AccountDiscount{Modules: []ModuleDiscount{module("bank", rule), module("bank", rule)}}, false},
		{"invalid nested rule", AccountDiscount{Modules: []ModuleDiscount{module("bank",
			Discount{DiscountType: FeeDiscountTypePercent, MsgType: testMsgType, Amount: sdkmath.LegacyNewDec(101)})}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAccountDiscount(tc.policy)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
