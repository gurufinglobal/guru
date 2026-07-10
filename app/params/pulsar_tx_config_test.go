package params

import (
	"bytes"
	"errors"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkbech32 "github.com/cosmos/cosmos-sdk/types/bech32"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
)

func TestShouldUsePulsarFallbackOnlyForGuruNestedMessageLookup(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "guru nested pulsar message",
			err:  errors.New(`decoding tx: failed to retrieve the message of type "guru.oracle.v1.OracleTask": tx parse error`),
			want: true,
		},
		{
			name: "non guru nested message",
			err:  errors.New(`decoding tx: failed to retrieve the message of type "cosmos.bank.v1beta1.MsgSend": tx parse error`),
			want: false,
		},
		{
			name: "ordinary decode error",
			err:  errors.New("txRaw must follow ADR-027"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUsePulsarFallback(tt.err); got != tt.want {
				t.Fatalf("shouldUsePulsarFallback() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestBexMsgSignersIncludeAdminWithDistinctFeePayerAndGranter(t *testing.T) {
	configureSDKBech32ForTest()

	encoding := MakeEncodingConfig(Bech32PrefixAccAddr, Bech32PrefixValAddr, Bech32PrefixConsAddr)
	bextypes.RegisterInterfaces(encoding.InterfaceRegistry)

	adminBytes := bytes.Repeat([]byte{0x11}, 20)
	feePayerBytes := bytes.Repeat([]byte{0x22}, 20)
	feeGranterBytes := bytes.Repeat([]byte{0x33}, 20)

	admin, err := sdkbech32.ConvertAndEncode(Bech32PrefixAccAddr, adminBytes)
	if err != nil {
		t.Fatalf("admin address: %v", err)
	}
	feePayer, err := sdkbech32.ConvertAndEncode(Bech32PrefixAccAddr, feePayerBytes)
	if err != nil {
		t.Fatalf("fee payer address: %v", err)
	}
	feeGranter, err := sdkbech32.ConvertAndEncode(Bech32PrefixAccAddr, feeGranterBytes)
	if err != nil {
		t.Fatalf("fee granter address: %v", err)
	}

	builder := encoding.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(&bexv1.MsgWithdrawFees{
		AdminAddress: admin,
		ExchangeId:   7,
		Amount:       []*basev1beta1.Coin{{Denom: "agxn", Amount: "1"}},
		Recipient:    admin,
	}); err != nil {
		t.Fatalf("set BEX msg: %v", err)
	}
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("agxn", sdkmath.NewInt(10))))
	builder.SetFeePayer(feePayerBytes)
	builder.SetFeeGranter(feeGranterBytes)

	txBytes, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatalf("encode tx: %v", err)
	}
	decodedTx, err := encoding.TxConfig.TxDecoder()(txBytes)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}

	sigTx, ok := decodedTx.(authsigning.SigVerifiableTx)
	if !ok {
		t.Fatalf("decoded tx %T does not implement SigVerifiableTx", decodedTx)
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		t.Fatalf("get signers: %v", err)
	}
	if len(signers) != 2 || !bytes.Equal(signers[0], adminBytes) || !bytes.Equal(signers[1], feePayerBytes) {
		t.Fatalf("unexpected signers: got=%X want admin=%X fee_payer=%X", signers, adminBytes, feePayerBytes)
	}
	for _, signer := range signers {
		if bytes.Equal(signer, feeGranterBytes) {
			t.Fatalf("fee granter must not replace the BEX admin signer: signers=%X granter=%X", signers, feeGranterBytes)
		}
	}

	feeTx, ok := decodedTx.(sdk.FeeTx)
	if !ok {
		t.Fatalf("decoded tx %T does not implement FeeTx", decodedTx)
	}
	if !bytes.Equal(feeTx.FeePayer(), feePayerBytes) {
		t.Fatalf("unexpected fee payer: got=%X want=%X", feeTx.FeePayer(), feePayerBytes)
	}
	if !bytes.Equal(feeTx.FeeGranter(), feeGranterBytes) {
		t.Fatalf("unexpected fee granter: got=%X want=%X", feeTx.FeeGranter(), feeGranterBytes)
	}
	if feePayer == admin || feeGranter == admin {
		t.Fatalf("test setup must use non-admin fee payer and granter")
	}
}

func TestBexAllMsgSignersIncludeAuthorityWithDistinctFeePayerAndGranter(t *testing.T) {
	configureSDKBech32ForTest()

	encoding := MakeEncodingConfig(Bech32PrefixAccAddr, Bech32PrefixValAddr, Bech32PrefixConsAddr)
	bextypes.RegisterInterfaces(encoding.InterfaceRegistry)

	moderatorBytes := bytes.Repeat([]byte{0x10}, 20)
	adminBytes := bytes.Repeat([]byte{0x11}, 20)
	otherAdminBytes := bytes.Repeat([]byte{0x12}, 20)
	recipientBytes := bytes.Repeat([]byte{0x13}, 20)
	feePayerBytes := bytes.Repeat([]byte{0x22}, 20)
	feeGranterBytes := bytes.Repeat([]byte{0x33}, 20)

	encodeAddress := func(name string, bz []byte) string {
		t.Helper()
		addr, err := sdkbech32.ConvertAndEncode(Bech32PrefixAccAddr, bz)
		if err != nil {
			t.Fatalf("%s address: %v", name, err)
		}
		return addr
	}

	moderator := encodeAddress("moderator", moderatorBytes)
	admin := encodeAddress("admin", adminBytes)
	otherAdmin := encodeAddress("other admin", otherAdminBytes)
	recipient := encodeAddress("recipient", recipientBytes)

	amount := []*basev1beta1.Coin{{Denom: "agxn", Amount: "1"}}
	tests := []struct {
		name      string
		msg       sdk.Msg
		authority []byte
	}{
		{
			name:      "register admin",
			msg:       &bexv1.MsgRegisterAdmin{Moderator: moderator, AdminAddress: otherAdmin},
			authority: moderatorBytes,
		},
		{
			name:      "remove admin",
			msg:       &bexv1.MsgRemoveAdmin{Moderator: moderator, AdminAddress: otherAdmin},
			authority: moderatorBytes,
		},
		{
			name: "register exchange",
			msg: &bexv1.MsgRegisterExchange{
				AdminAddress:              admin,
				DenomA:                    "ibc/gxusd",
				DenomB:                    "ibc/gxkrw",
				PortA:                     "transwap",
				ChannelA:                  "channel-1",
				PortB:                     "transwap",
				ChannelB:                  "channel-2",
				OracleSymbolAToB:          "GXUSD/GXKRW",
				OracleSymbolBToA:          "GXKRW/GXUSD",
				VolumeEpochSeconds:        60,
				MaxOracleStalenessSeconds: 30,
			},
			authority: adminBytes,
		},
		{
			name:      "update exchange",
			msg:       &bexv1.MsgUpdateExchange{AdminAddress: admin, ExchangeId: 7, ExpectedRevision: 1, Patch: &bexv1.ExchangeUpdatePatch{}},
			authority: adminBytes,
		},
		{
			name:      "delete exchange",
			msg:       &bexv1.MsgDeleteExchange{AdminAddress: admin, ExchangeId: 7},
			authority: adminBytes,
		},
		{
			name:      "deposit reserve",
			msg:       &bexv1.MsgDepositReserve{AdminAddress: admin, ExchangeId: 7, Amount: amount},
			authority: adminBytes,
		},
		{
			name:      "withdraw reserve",
			msg:       &bexv1.MsgWithdrawReserve{AdminAddress: admin, ExchangeId: 7, Amount: amount, Recipient: recipient},
			authority: adminBytes,
		},
		{
			name:      "withdraw fees",
			msg:       &bexv1.MsgWithdrawFees{AdminAddress: admin, ExchangeId: 7, Amount: amount, Recipient: recipient},
			authority: adminBytes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := encoding.TxConfig.NewTxBuilder()
			if err := builder.SetMsgs(tt.msg); err != nil {
				t.Fatalf("set BEX msg: %v", err)
			}
			builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("agxn", sdkmath.NewInt(10))))
			builder.SetFeePayer(feePayerBytes)
			builder.SetFeeGranter(feeGranterBytes)

			txBytes, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
			if err != nil {
				t.Fatalf("encode tx: %v", err)
			}
			decodedTx, err := encoding.TxConfig.TxDecoder()(txBytes)
			if err != nil {
				t.Fatalf("decode tx: %v", err)
			}

			sigTx, ok := decodedTx.(authsigning.SigVerifiableTx)
			if !ok {
				t.Fatalf("decoded tx %T does not implement SigVerifiableTx", decodedTx)
			}
			signers, err := sigTx.GetSigners()
			if err != nil {
				t.Fatalf("get signers: %v", err)
			}
			if len(signers) != 2 || !bytes.Equal(signers[0], tt.authority) || !bytes.Equal(signers[1], feePayerBytes) {
				t.Fatalf("unexpected signers: got=%X want authority=%X fee_payer=%X", signers, tt.authority, feePayerBytes)
			}
			for _, signer := range signers {
				if bytes.Equal(signer, feeGranterBytes) {
					t.Fatalf("fee granter must not replace the BEX authority signer: signers=%X granter=%X", signers, feeGranterBytes)
				}
			}

			feeTx, ok := decodedTx.(sdk.FeeTx)
			if !ok {
				t.Fatalf("decoded tx %T does not implement FeeTx", decodedTx)
			}
			if !bytes.Equal(feeTx.FeePayer(), feePayerBytes) {
				t.Fatalf("unexpected fee payer: got=%X want=%X", feeTx.FeePayer(), feePayerBytes)
			}
			if !bytes.Equal(feeTx.FeeGranter(), feeGranterBytes) {
				t.Fatalf("unexpected fee granter: got=%X want=%X", feeTx.FeeGranter(), feeGranterBytes)
			}
		})
	}
}

func configureSDKBech32ForTest() {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
}
