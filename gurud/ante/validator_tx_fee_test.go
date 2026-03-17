package ante

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	cosmosante "github.com/gurufinglobal/guru/v2/ante/cosmos"
	"github.com/gurufinglobal/guru/v2/encoding"
)

func TestCheckTxFeeWithValidatorMinGasPrices_UsesEffectiveGas(t *testing.T) {
	encCfg := encoding.MakeConfig(1)

	from := sdk.AccAddress([]byte("from_______________"))
	to := sdk.AccAddress([]byte("to_________________"))
	minGasPrice := math.LegacyNewDec(10)

	ctx := sdk.Context{}.
		WithIsCheckTx(true).
		WithMinGasPrices(sdk.DecCoins{{
			Denom:  "aguru",
			Amount: minGasPrice,
		}})

	t.Run("single MsgSend uses fixed 21000 for fee checks and priority", func(t *testing.T) {
		sendMsg := banktypes.NewMsgSend(
			from,
			to,
			sdk.NewCoins(sdk.NewInt64Coin("aguru", 1)),
		)
		tx := buildValidatorFeeTestTx(
			t,
			encCfg.TxConfig,
			100_000,
			sdk.NewCoins(sdk.NewCoin("aguru", math.NewInt(210_000))),
			sendMsg,
		)

		_, _, priority, err := checkTxFeeWithValidatorMinGasPrices(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, int64(10), priority)
	})

	t.Run("non target msgs still use tx gas for fee checks", func(t *testing.T) {
		multiSendMsg := &banktypes.MsgMultiSend{
			Inputs: []banktypes.Input{
				{
					Address: from.String(),
					Coins:   sdk.NewCoins(sdk.NewInt64Coin("aguru", 1)),
				},
			},
			Outputs: []banktypes.Output{
				{
					Address: to.String(),
					Coins:   sdk.NewCoins(sdk.NewInt64Coin("aguru", 1)),
				},
			},
		}

		tx := buildValidatorFeeTestTx(
			t,
			encCfg.TxConfig,
			100_000,
			sdk.NewCoins(sdk.NewCoin("aguru", math.NewInt(210_000))),
			multiSendMsg,
		)

		_, _, _, err := checkTxFeeWithValidatorMinGasPrices(ctx, tx)
		require.Error(t, err)
		require.ErrorContains(t, err, "insufficient fees")
	})

	t.Run("single MsgSend with exact required fee from fixed gas passes", func(t *testing.T) {
		sendMsg := banktypes.NewMsgSend(
			from,
			to,
			sdk.NewCoins(sdk.NewInt64Coin("aguru", 2)),
		)
		required := math.NewInt(int64(cosmosante.FixedMsgSendGas)).
			Mul(minGasPrice.TruncateInt())

		tx := buildValidatorFeeTestTx(
			t,
			encCfg.TxConfig,
			200_000,
			sdk.NewCoins(sdk.NewCoin("aguru", required)),
			sendMsg,
		)

		_, _, _, err := checkTxFeeWithValidatorMinGasPrices(ctx, tx)
		require.NoError(t, err)
	})
}

func buildValidatorFeeTestTx(
	t *testing.T,
	txCfg client.TxConfig,
	gasLimit uint64,
	fees sdk.Coins,
	msgs ...sdk.Msg,
) sdk.Tx {
	t.Helper()

	txBuilder := txCfg.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msgs...))
	txBuilder.SetGasLimit(gasLimit)
	txBuilder.SetFeeAmount(fees)
	return txBuilder.GetTx()
}
