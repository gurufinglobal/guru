package app

import (
	"bytes"
	"context"
	"math/big"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	antetypes "github.com/cosmos/evm/ante/types"
	appante "github.com/gurufinglobal/guru/v3/app/ante"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

func maxAmountCoins(
	t *testing.T,
	baseDenom string,
	denomLen int,
	memo string,
	withExt bool,
	fixedFeeDigits int,
	maxPriorityPriceLen int,
) (int, int) {
	t.Helper()
	const maxSdkIntDigits = 77
	config := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	banktypes.RegisterInterfaces(config.InterfaceRegistry)

	priv := secp256k1.GenPrivKeyFromSecret([]byte("standard-msgsend-max-simulation-key-01"))
	sender := types.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	recipient := types.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	senderStr, err := types.Bech32ifyAddressBytes(appparams.Bech32PrefixAccAddr, sender)
	if err != nil {
		t.Fatalf("bech32 sender: %v", err)
	}

	build := func(amtDigits, feeDigits, priorityDigits int, withExt bool) (int, bool) {
		denom := "a" + strings.Repeat("b", denomLen-1)
		amount, ok := intWithDigits(amtDigits, maxSdkIntDigits)
		if !ok {
			return 0, false
		}
		feeAmount, ok := intWithDigits(feeDigits, maxSdkIntDigits)
		if !ok {
			return 0, false
		}

		builder := config.TxConfig.NewTxBuilder()
		msg := banktypes.NewMsgSend(
			sender,
			recipient,
			types.NewCoins(types.NewCoin(denom, amount)),
		)
		if err := builder.SetMsgs(msg); err != nil {
			t.Fatalf("set msg: %v", err)
		}
		builder.SetGasLimit(appante.StandardMsgSendGas)
		builder.SetFeeAmount(types.NewCoins(types.NewCoin(baseDenom, feeAmount)))
		builder.SetMemo(memo)

		if withExt {
			priorityRaw, ok := intWithDigits(priorityDigits, maxSdkIntDigits)
			if !ok {
				return 0, false
			}
			priority := sdkmath.LegacyNewDecFromInt(priorityRaw)
			ext, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{
				MaxPriorityPrice: priority,
			})
			if err != nil {
				t.Fatalf("pack ext: %v", err)
			}
			extBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder)
			if !ok {
				t.Fatalf("extension builder unsupported")
			}
			extBuilder.SetExtensionOptions(ext)
		}

		signMode, err := authsigning.APISignModeToInternal(config.TxConfig.SignModeHandler().DefaultMode())
		if err != nil {
			t.Fatalf("sign mode: %v", err)
		}
		sig := signing.SignatureV2{
			PubKey: priv.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode: signMode,
			},
			Sequence: 0,
		}
		if err := builder.SetSignatures(sig); err != nil {
			t.Fatalf("placeholder signature: %v", err)
		}

		signerData := authsigning.SignerData{
			Address:       senderStr,
			ChainID:       "sim-chain",
			AccountNumber: 0,
			Sequence:      0,
			PubKey:        priv.PubKey(),
		}
		signBytes, err := authsigning.GetSignBytesAdapter(
			context.Background(),
			config.TxConfig.SignModeHandler(),
			signMode,
			signerData,
			builder.GetTx(),
		)
		if err != nil {
			t.Fatalf("sign bytes: %v", err)
		}
		rawSig, err := priv.Sign(signBytes)
		if err != nil {
			t.Fatalf("sign tx: %v", err)
		}
		sig.Data.(*signing.SingleSignatureData).Signature = rawSig
		if err := builder.SetSignatures(sig); err != nil {
			t.Fatalf("final signature: %v", err)
		}

		txBytes, err := config.TxConfig.TxEncoder()(builder.GetTx())
		if err != nil {
			t.Fatalf("encode tx: %v", err)
		}
		return len(txBytes), true
	}

	lo, hi := 1, maxSdkIntDigits
	bestSize := 0
	for lo <= hi {
		mid := (lo + hi) / 2
		size, ok := build(mid, fixedFeeDigits, maxPriorityPriceLen, withExt)
		if !ok {
			hi = mid - 1
			continue
		}
		bestSize = size
		lo = mid + 1
	}
	return hi, bestSize
}

func intWithDigits(digits int, maxDigits int) (value sdkmath.Int, ok bool) {
	ok = true
	if digits < 1 || digits > maxDigits {
		return sdkmath.Int{}, false
	}
	var base big.Int
	base.Exp(big.NewInt(10), big.NewInt(int64(digits-1)), nil)
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	value = sdkmath.NewIntFromBigInt(&base)
	return value, ok
}

func TestSimulateMaxMsgSendPayloadCurrentCaps(t *testing.T) {
	configureFeePolicyTestBech32Prefixes(t, true)

	baseDenom := appparams.BaseDenom

	var memo strings.Builder
	for i := 0; i < appante.StandardMsgSendMaxMemoBytes; i++ {
		memo.WriteByte('m')
	}
	minMemo := ""

	maxAmountNoMemoNoExt, maxSizeNoMemoNoExt := maxAmountCoins(t, baseDenom, 3, minMemo, false, 1, 1)
	maxAmountWithMemoNoExt, maxSizeWithMemoNoExt := maxAmountCoins(t, baseDenom, 3, memo.String(), false, 1, 1)
	maxAmountWithLongDenomNoExt, maxSizeWithLongDenomNoExt := maxAmountCoins(
		t,
		baseDenom,
		128,
		minMemo,
		false,
		1,
		1,
	)
	maxAmountWithLongDenomAndMemoNoExt, maxSizeWithLongDenomAndMemoNoExt := maxAmountCoins(
		t,
		baseDenom,
		128,
		memo.String(),
		false,
		1,
		1,
	)
	maxAmountNoMemoWithExt, maxSizeNoMemoWithExt := maxAmountCoins(t, baseDenom, 3, minMemo, true, 1, 77)
	maxAmountWithMemoWithExt, maxSizeWithMemoWithExt := maxAmountCoins(t, baseDenom, 3, memo.String(), true, 1, 77)
	maxAmountLongDenomWithMemoWithExt, maxSizeLongDenomWithMemoWithExt := maxAmountCoins(
		t,
		baseDenom,
		128,
		memo.String(),
		true,
		1,
		77,
	)

	maxAmountNoMemoWithExtBigFee, maxSizeNoMemoWithExtBigFee := maxAmountCoins(
		t,
		baseDenom,
		128,
		minMemo,
		true,
		77,
		77,
	)
	maxAmountWithMemoAndExtBigFee, maxSizeWithMemoAndExtBigFee := maxAmountCoins(
		t,
		baseDenom,
		128,
		memo.String(),
		true,
		77,
		77,
	)
	maxAmountWithLongDenomAndMemoNoExtBigFee, maxSizeWithLongDenomAndMemoNoExtBigFee := maxAmountCoins(
		t,
		baseDenom,
		128,
		memo.String(),
		false,
		77,
		77,
	)

	t.Logf("baseline max amount digits (denom min, memo=0, no ext): %d (tx size=%d)", maxAmountNoMemoNoExt, maxSizeNoMemoNoExt)
	t.Logf("max amount digits (denom min, memo=256, no ext): %d (tx size=%d)", maxAmountWithMemoNoExt, maxSizeWithMemoNoExt)
	t.Logf("max amount digits (denom 128, memo=0, no ext): %d (tx size=%d)", maxAmountWithLongDenomNoExt, maxSizeWithLongDenomNoExt)
	t.Logf("max amount digits (denom 128, memo=256, no ext): %d (tx size=%d)", maxAmountWithLongDenomAndMemoNoExt, maxSizeWithLongDenomAndMemoNoExt)
	t.Logf("max amount digits (denom min, memo=0, ext maxPriority 77): %d (tx size=%d)", maxAmountNoMemoWithExt, maxSizeNoMemoWithExt)
	t.Logf("max amount digits (denom min, memo=256, ext maxPriority 77): %d (tx size=%d)", maxAmountWithMemoWithExt, maxSizeWithMemoWithExt)
	t.Logf("max amount digits (denom 128, memo=256, ext maxPriority 77): %d (tx size=%d)", maxAmountLongDenomWithMemoWithExt, maxSizeLongDenomWithMemoWithExt)
	t.Logf("max amount digits (denom 128, memo=0, ext maxPriority 77, fee 77 digits): %d (tx size=%d)", maxAmountNoMemoWithExtBigFee, maxSizeNoMemoWithExtBigFee)
	t.Logf("max amount digits (denom 128, memo=256, no ext, fee 77 digits): %d (tx size=%d)", maxAmountWithLongDenomAndMemoNoExtBigFee, maxSizeWithLongDenomAndMemoNoExtBigFee)
	t.Logf("max amount digits (denom 128, memo=256, ext maxPriority 77, fee 77 digits): %d (tx size=%d)", maxAmountWithMemoAndExtBigFee, maxSizeWithMemoAndExtBigFee)
}
