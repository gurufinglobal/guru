package keeper

import (
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestValidateIntStringParsesDecimalOnly(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "leading zero", value: "010", want: "10"},
		{name: "invalid octal digit is decimal", value: "08", want: "8"},
		{name: "explicit plus", value: "+010", want: "10"},
		{name: "uint256 max", value: maxUint256String, want: maxUint256String},
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
				amount, parseErr := validateRequiredIntString("amount", tc.value)
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

func TestDecimalAmountsAreCanonicalAcrossStateAndCoins(t *testing.T) {
	require.Equal(t, "10", normalizeIntString("010"))
	require.Equal(t, "8", normalizeIntString("08"))
	require.Equal(t, "0x10", normalizeIntString("0x10"))

	coins, err := protoCoinsToSDK([]*basev1beta1.Coin{{Denom: "agxn", Amount: "010"}})
	require.NoError(t, err)
	require.Equal(t, "10agxn", coins.String())

	_, err = protoCoinsToSDK([]*basev1beta1.Coin{{Denom: "agxn", Amount: "0x10"}})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

func TestQuoteSwapAcceptsEightAsDecimalWithoutPanic(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	f.oracleKeeper.SetValue(exchange.GetOracleSymbolAToB(), "1", f.ctx.BlockTime().Unix())

	var (
		quote *bexv1.QuoteSwapResponse
		err   error
	)
	require.NotPanics(t, func() {
		quote, err = f.keeper.QuoteSwap(f.ctx, &bexv1.QuoteSwapRequest{
			ExchangeId: exchange.GetId(),
			InputDenom: exchange.GetDenomA(),
			AmountIn:   "08",
		})
	})
	require.NoError(t, err)
	require.Equal(t, "8", quote.GetAmountIn())
}
