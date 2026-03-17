package cosmos_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	cosmomultisig "github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	signing "github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	cosmosante "github.com/gurufinglobal/guru/v2/ante/cosmos"
	"github.com/gurufinglobal/guru/v2/encoding"
	"github.com/gurufinglobal/guru/v2/testutil"
)

func TestFixedSendGasDecorator(t *testing.T) {
	encCfg := encoding.MakeConfig(1)
	dec := cosmosante.NewFixedSendGasDecorator()

	from := sdk.AccAddress([]byte("from_______________"))
	to := sdk.AccAddress([]byte("to_________________"))

	sendMsg := banktypes.NewMsgSend(
		from,
		to,
		sdk.NewCoins(sdk.NewInt64Coin("aguru", 1)),
	)

	t.Run("single MsgSend uses fixed 21000 gas", func(t *testing.T) {
		tx := buildTestTx(t, encCfg.TxConfig, 500_000, "memo is ignored for fixed gas", sendMsg)
		ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(500_000))

		newCtx, err := dec.AnteHandle(ctx, tx, false, testutil.NoOpNextFn)
		require.NoError(t, err)
		require.Equal(t, uint64(cosmosante.FixedMsgSendGas), uint64(newCtx.GasMeter().Limit()))
		require.Equal(t, uint64(cosmosante.FixedMsgSendGas), uint64(newCtx.GasMeter().GasConsumed()))

		require.NotPanics(t, func() {
			newCtx.GasMeter().ConsumeGas(1_000_000, "must remain fixed")
			newCtx.GasMeter().RefundGas(1_000_000, "must remain fixed")
		})
		require.Equal(t, uint64(cosmosante.FixedMsgSendGas), uint64(newCtx.GasMeter().GasConsumed()))
	})

	t.Run("single MsgSend rejects gas limit below 21000 on non-simulate", func(t *testing.T) {
		tx := buildTestTx(t, encCfg.TxConfig, cosmosante.FixedMsgSendGas-1, "", sendMsg)
		ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(500_000))

		_, err := dec.AnteHandle(ctx, tx, false, testutil.NoOpNextFn)
		require.Error(t, err)
		require.ErrorIs(t, err, sdkerrors.ErrOutOfGas)
	})

	t.Run("single MsgSend simulation allows low tx gas and still fixes to 21000", func(t *testing.T) {
		tx := buildTestTx(t, encCfg.TxConfig, 0, "", sendMsg)
		ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(500_000))

		newCtx, err := dec.AnteHandle(ctx, tx, true, testutil.NoOpNextFn)
		require.NoError(t, err)
		require.Equal(t, uint64(cosmosante.FixedMsgSendGas), uint64(newCtx.GasMeter().GasConsumed()))
	})

	t.Run("non-target tx keeps original gas meter", func(t *testing.T) {
		msg2 := banktypes.NewMsgSend(
			from,
			to,
			sdk.NewCoins(sdk.NewInt64Coin("aguru", 2)),
		)
		tx := buildTestTx(t, encCfg.TxConfig, 500_000, "", sendMsg, msg2)
		ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(500_000))

		newCtx, err := dec.AnteHandle(ctx, tx, false, testutil.NoOpNextFn)
		require.NoError(t, err)
		require.Equal(t, uint64(500_000), uint64(newCtx.GasMeter().Limit()))
		require.Equal(t, uint64(0), uint64(newCtx.GasMeter().GasConsumed()))
	})

	t.Run("single MsgSend with multisig signature still uses fixed gas", func(t *testing.T) {
		priv1 := secp256k1.GenPrivKey()
		priv2 := secp256k1.GenPrivKey()
		pubKeys := []cryptotypes.PubKey{priv1.PubKey(), priv2.PubKey()}
		multiSigPubKey := multisig.NewLegacyAminoPubKey(2, pubKeys)

		multiSigSend := banktypes.NewMsgSend(
			sdk.AccAddress(multiSigPubKey.Address()),
			to,
			sdk.NewCoins(sdk.NewInt64Coin("aguru", 1)),
		)

		txBuilder := encCfg.TxConfig.NewTxBuilder()
		require.NoError(t, txBuilder.SetMsgs(multiSigSend))
		txBuilder.SetGasLimit(500_000)
		txBuilder.SetMemo("multisig send memo")
		txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("aguru", 500_000)))

		multiSigData := cosmomultisig.NewMultisig(2)
		sig1 := signing.SignatureV2{
			PubKey: priv1.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
				Signature: []byte{0x01},
			},
			Sequence: 0,
		}
		sig2 := signing.SignatureV2{
			PubKey: priv2.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
				Signature: []byte{0x02},
			},
			Sequence: 0,
		}

		require.NoError(t, cosmomultisig.AddSignatureV2(multiSigData, sig1, pubKeys))
		require.NoError(t, cosmomultisig.AddSignatureV2(multiSigData, sig2, pubKeys))
		require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
			PubKey:   multiSigPubKey,
			Data:     multiSigData,
			Sequence: 0,
		}))

		tx := txBuilder.GetTx()
		ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(500_000))

		newCtx, err := dec.AnteHandle(ctx, tx, false, testutil.NoOpNextFn)
		require.NoError(t, err)
		require.Equal(t, uint64(cosmosante.FixedMsgSendGas), uint64(newCtx.GasMeter().GasConsumed()))
	})
}

func buildTestTx(
	t *testing.T,
	txCfg client.TxConfig,
	gasLimit uint64,
	memo string,
	msgs ...sdk.Msg,
) sdk.Tx {
	t.Helper()

	txBuilder := txCfg.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msgs...))
	txBuilder.SetGasLimit(gasLimit)
	txBuilder.SetMemo(memo)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("aguru", 500_000)))

	return txBuilder.GetTx()
}
