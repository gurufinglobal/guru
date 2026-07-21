package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestExchangeLimitRequiresCanonicalDecimalOrEmpty(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "empty is unlimited", value: "", want: "0"},
		{name: "zero", value: "0", want: "0"},
		{name: "decimal", value: "10", want: "10"},
		{name: "uint256 max", value: maxUint256String, want: maxUint256String},
		{name: "leading zero", value: "010", wantErr: true},
		{name: "invalid octal digit", value: "08", wantErr: true},
		{name: "explicit plus", value: "+010", wantErr: true},
		{name: "whitespace", value: " 10 ", wantErr: true},
		{name: "hex prefix", value: "0x10", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "uint256 overflow", value: "115792089237316195423570985008687907853269984665640564039457584007913129639936", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				got string
				err error
			)
			require.NotPanics(t, func() {
				amount, parseErr := validateExchangeLimitIntString("amount", tc.value)
				err = parseErr
				if parseErr == nil {
					got = amount.String()
				}
			})
			if tc.wantErr {
				require.ErrorIs(t, err, types.ErrInvalidRequest)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestValidateRequiredIntStringRequiresCanonicalDecimal(t *testing.T) {
	for _, valid := range []string{"0", "8", "10", maxUint256String} {
		t.Run("valid_"+valid, func(t *testing.T) {
			amount, err := validateRequiredIntString("amount", valid)
			require.NoError(t, err)
			require.Equal(t, valid, amount.String())
		})
	}

	for _, invalid := range []string{
		"",
		"00",
		"08",
		"010",
		"+10",
		"-10",
		" 10",
		"10 ",
		"0x10",
		"0o10",
		"1_0",
		"1e1",
		"１０",
		"115792089237316195423570985008687907853269984665640564039457584007913129639936",
	} {
		t.Run("invalid_"+invalid, func(t *testing.T) {
			_, err := validateRequiredIntString("amount", invalid)
			require.ErrorIs(t, err, types.ErrInvalidRequest)
		})
	}
}

func TestDecimalAmountsAreCanonicalAcrossStateAndSDKCoins(t *testing.T) {
	require.Equal(t, "0", normalizeIntString(""))
	require.Equal(t, "10", normalizeIntString("10"))
	require.Equal(t, "010", normalizeIntString("010"))
	require.Equal(t, "08", normalizeIntString("08"))
	require.Equal(t, "0x10", normalizeIntString("0x10"))

	coins, err := protoCoinsToSDK(sdk.Coins{
		sdk.NewInt64Coin("gxusd", 2),
		sdk.NewInt64Coin("agxn", 10),
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 10), sdk.NewInt64Coin("gxusd", 2)), coins)

	for name, invalid := range map[string]sdk.Coins{
		"nil amount":    {{Denom: "agxn", Amount: sdkmath.Int{}}},
		"zero amount":   {{Denom: "agxn", Amount: sdkmath.ZeroInt()}},
		"negative":      {{Denom: "agxn", Amount: sdkmath.NewInt(-1)}},
		"invalid denom": {{Denom: "bad denom", Amount: sdkmath.OneInt()}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := protoCoinsToSDK(invalid)
			require.ErrorIs(t, err, types.ErrInvalidRequest)
		})
	}
}

func TestQuoteSwapRejectsNonCanonicalAmountWithoutPanic(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	f.oracleKeeper.SetValue(exchange.GetOracleSymbolAToB(), "1", f.ctx.BlockTime().Unix())

	for _, amount := range []string{"08", "010", "+10", " 10", "10 ", "0x10", "0o10"} {
		t.Run(amount, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := f.keeper.QuoteSwap(f.ctx, &types.QuoteSwapRequest{
					ExchangeId: exchange.GetId(),
					InputDenom: exchange.GetDenomA(),
					AmountIn:   amount,
				})
				require.ErrorIs(t, err, types.ErrInvalidRequest)
			})
		})
	}

	quote, err := f.keeper.QuoteSwap(f.ctx, &types.QuoteSwapRequest{
		ExchangeId: exchange.GetId(),
		InputDenom: exchange.GetDenomA(),
		AmountIn:   "8",
	})
	require.NoError(t, err)
	require.Equal(t, "8", quote.GetAmountIn())
}
