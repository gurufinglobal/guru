package params

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec/unknownproto"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkbech32 "github.com/cosmos/cosmos-sdk/types/bech32"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	gogotypes "github.com/cosmos/gogoproto/types"
	"google.golang.org/protobuf/encoding/protowire"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestTransSwapUpdateParamsRoundTripsThroughStandardTxConfig(t *testing.T) {
	configureSDKBech32ForTest()

	encoding := MakeEncodingConfig(Bech32PrefixAccAddr, Bech32PrefixValAddr, Bech32PrefixConsAddr)
	transwaptypes.RegisterInterfaces(encoding.InterfaceRegistry)

	authorityBytes := bytes.Repeat([]byte{0x41}, 20)
	feePayerBytes := bytes.Repeat([]byte{0x42}, 20)
	authority := encodeTestAddress(t, authorityBytes)

	builder := encoding.TxConfig.NewTxBuilder()
	msg := &transwaptypes.MsgUpdateParams{
		Authority: authority,
		Params:    transwaptypes.DefaultParams(),
	}
	if err := builder.SetMsgs(msg); err != nil {
		t.Fatalf("set TransSwap update params: %v", err)
	}
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("agxn", sdkmath.NewInt(10))))
	builder.SetFeePayer(feePayerBytes)

	txBytes, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatalf("encode tx: %v", err)
	}
	decodedTx, err := encoding.TxConfig.TxDecoder()(txBytes)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}

	decodedMsgs := decodedTx.GetMsgs()
	if len(decodedMsgs) != 1 {
		t.Fatalf("unexpected decoded message count: %d", len(decodedMsgs))
	}
	decodedMsg, ok := decodedMsgs[0].(*transwaptypes.MsgUpdateParams)
	if !ok {
		t.Fatalf("unexpected decoded message type: %T", decodedMsgs[0])
	}
	if decodedMsg.GetAuthority() != authority || decodedMsg.GetParams() == nil {
		t.Fatalf("unexpected decoded TransSwap update params: %+v", decodedMsg)
	}

	msgsV2, err := decodedTx.GetMsgsV2()
	if err != nil {
		t.Fatalf("adapt TransSwap message to protobuf v2: %v", err)
	}
	if len(msgsV2) != 1 || msgsV2[0].ProtoReflect().Descriptor().FullName() != "guru.transwap.v1.MsgUpdateParams" {
		t.Fatalf("unexpected protobuf v2 messages: %+v", msgsV2)
	}

	assertDecodedTxSigners(t, decodedTx, authorityBytes, feePayerBytes, nil)
	assertDeterministicTxReencoding(t, encoding.TxConfig, decodedTx, txBytes)
	assertStandardTxJSON(t, encoding.TxConfig, decodedTx)
}

func TestBexAllMessagesRoundTripThroughStandardTxConfig(t *testing.T) {
	configureSDKBech32ForTest()

	encoding := MakeEncodingConfig(Bech32PrefixAccAddr, Bech32PrefixValAddr, Bech32PrefixConsAddr)
	bextypes.RegisterInterfaces(encoding.InterfaceRegistry)

	moderatorBytes := bytes.Repeat([]byte{0x10}, 20)
	adminBytes := bytes.Repeat([]byte{0x11}, 20)
	otherAdminBytes := bytes.Repeat([]byte{0x12}, 20)
	replacementAdminBytes := bytes.Repeat([]byte{0x13}, 20)
	recipientBytes := bytes.Repeat([]byte{0x14}, 20)
	feePayerBytes := bytes.Repeat([]byte{0x22}, 20)
	feeGranterBytes := bytes.Repeat([]byte{0x33}, 20)

	moderator := encodeTestAddress(t, moderatorBytes)
	admin := encodeTestAddress(t, adminBytes)
	otherAdmin := encodeTestAddress(t, otherAdminBytes)
	replacementAdmin := encodeTestAddress(t, replacementAdminBytes)
	recipient := encodeTestAddress(t, recipientBytes)

	amount := sdk.NewCoins(sdk.NewCoin("agxn", sdkmath.NewInt(1)))
	tests := []struct {
		name      string
		msg       sdk.Msg
		authority []byte
	}{
		{
			name:      "register admin",
			msg:       &bextypes.MsgRegisterAdmin{Moderator: moderator, AdminAddress: otherAdmin},
			authority: moderatorBytes,
		},
		{
			name: "update admin",
			msg: &bextypes.MsgUpdateAdmin{
				Moderator:       moderator,
				OldAdminAddress: otherAdmin,
				NewAdminAddress: replacementAdmin,
			},
			authority: moderatorBytes,
		},
		{
			name:      "remove admin",
			msg:       &bextypes.MsgRemoveAdmin{Moderator: moderator, AdminAddress: otherAdmin},
			authority: moderatorBytes,
		},
		{
			name: "register exchange",
			msg: &bextypes.MsgRegisterExchange{
				BexAdminAddress:           admin,
				ExchangeAdminAddress:      admin,
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
				Metadata:                  map[string]string{"route": "direct"},
			},
			authority: adminBytes,
		},
		{
			name: "update exchange with nested wrappers and metadata",
			msg: &bextypes.MsgUpdateExchange{
				AdminAddress:     admin,
				ExchangeId:       7,
				ExpectedRevision: 1,
				Patch: &bextypes.ExchangeUpdatePatch{
					DenomA:     &gogotypes.StringValue{Value: "ibc/new-gxusd"},
					FeeBpsAToB: &gogotypes.UInt32Value{Value: 7},
					Metadata:   map[string]string{"network": "test"},
				},
			},
			authority: adminBytes,
		},
		{
			name:      "delete exchange",
			msg:       &bextypes.MsgDeleteExchange{AdminAddress: admin, ExchangeId: 7},
			authority: adminBytes,
		},
		{
			name: "add reserve depositor",
			msg: &bextypes.MsgAddReserveDepositor{
				AdminAddress:     admin,
				ExchangeId:       7,
				DepositorAddress: recipient,
			},
			authority: adminBytes,
		},
		{
			name: "remove reserve depositor",
			msg: &bextypes.MsgRemoveReserveDepositor{
				AdminAddress:     admin,
				ExchangeId:       7,
				DepositorAddress: recipient,
			},
			authority: adminBytes,
		},
		{
			name:      "deposit reserve",
			msg:       &bextypes.MsgDepositReserve{Sender: admin, ExchangeId: 7, Amount: amount},
			authority: adminBytes,
		},
		{
			name:      "withdraw reserve",
			msg:       &bextypes.MsgWithdrawReserve{AdminAddress: admin, ExchangeId: 7, Amount: amount, Recipient: recipient},
			authority: adminBytes,
		},
		{
			name:      "withdraw fees",
			msg:       &bextypes.MsgWithdrawFees{AdminAddress: admin, ExchangeId: 7, Amount: amount, Recipient: recipient},
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
			if len(decodedTx.GetMsgs()) != 1 || reflect.TypeOf(decodedTx.GetMsgs()[0]) != reflect.TypeOf(tt.msg) {
				t.Fatalf("decoded BEX message type = %T, want %T", decodedTx.GetMsgs()[0], tt.msg)
			}

			msgsV2, err := decodedTx.GetMsgsV2()
			if err != nil {
				t.Fatalf("adapt BEX message to protobuf v2: %v", err)
			}
			if len(msgsV2) != 1 {
				t.Fatalf("unexpected protobuf v2 message count: %d", len(msgsV2))
			}

			assertDecodedTxSigners(t, decodedTx, tt.authority, feePayerBytes, feeGranterBytes)
			assertDeterministicTxReencoding(t, encoding.TxConfig, decodedTx, txBytes)
			assertStandardTxJSON(t, encoding.TxConfig, decodedTx)

			if decoded, ok := decodedTx.GetMsgs()[0].(*bextypes.MsgUpdateExchange); ok {
				if decoded.GetPatch().GetDenomA().GetValue() != "ibc/new-gxusd" ||
					decoded.GetPatch().GetFeeBpsAToB().GetValue() != 7 ||
					decoded.GetPatch().GetMetadata()["network"] != "test" {
					t.Fatalf("decoded BEX update patch lost nested data: %+v", decoded.GetPatch())
				}
			}
		})
	}
}

func TestBexMapEntriesRetainStrictUnknownFieldValidation(t *testing.T) {
	encoding := MakeEncodingConfig(Bech32PrefixAccAddr, Bech32PrefixValAddr, Bech32PrefixConsAddr)

	entryBytes := protowire.AppendTag(nil, 1, protowire.BytesType)
	entryBytes = protowire.AppendString(entryBytes, "network")
	entryBytes = protowire.AppendTag(entryBytes, 2, protowire.BytesType)
	entryBytes = protowire.AppendString(entryBytes, "test")
	entryBytes = protowire.AppendTag(entryBytes, 3, protowire.VarintType)
	entryBytes = protowire.AppendVarint(entryBytes, 1)

	patchBytes := protowire.AppendTag(nil, 16, protowire.BytesType)
	patchBytes = protowire.AppendBytes(patchBytes, entryBytes)
	err := unknownproto.RejectUnknownFieldsStrict(
		patchBytes,
		&bextypes.ExchangeUpdatePatch{},
		encoding.InterfaceRegistry,
	)
	if err == nil || !strings.Contains(err.Error(), "TagNum: 3") {
		t.Fatalf("strict decoder accepted an unknown map-entry field: %v", err)
	}
}

func assertDecodedTxSigners(t *testing.T, tx sdk.Tx, authority, feePayer, feeGranter []byte) {
	t.Helper()

	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		t.Fatalf("decoded tx %T does not implement SigVerifiableTx", tx)
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		t.Fatalf("get signers: %v", err)
	}
	if len(signers) != 2 || !bytes.Equal(signers[0], authority) || !bytes.Equal(signers[1], feePayer) {
		t.Fatalf("unexpected signers: got=%X want authority=%X fee_payer=%X", signers, authority, feePayer)
	}
	for _, signer := range signers {
		if len(feeGranter) > 0 && bytes.Equal(signer, feeGranter) {
			t.Fatalf("fee granter must not replace the message signer: signers=%X granter=%X", signers, feeGranter)
		}
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		t.Fatalf("decoded tx %T does not implement FeeTx", tx)
	}
	if !bytes.Equal(feeTx.FeePayer(), feePayer) {
		t.Fatalf("unexpected fee payer: got=%X want=%X", feeTx.FeePayer(), feePayer)
	}
	if !bytes.Equal(feeTx.FeeGranter(), feeGranter) {
		t.Fatalf("unexpected fee granter: got=%X want=%X", feeTx.FeeGranter(), feeGranter)
	}
}

func assertDeterministicTxReencoding(t *testing.T, txConfig client.TxConfig, tx sdk.Tx, want []byte) {
	t.Helper()

	builder, err := txConfig.WrapTxBuilder(tx)
	if err != nil {
		t.Fatalf("wrap decoded tx with the standard builder: %v", err)
	}
	reencoded, err := txConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatalf("re-encode decoded tx: %v", err)
	}
	if !bytes.Equal(reencoded, want) {
		t.Fatalf("standard TxConfig re-encoding changed transaction bytes: got=%X want=%X", reencoded, want)
	}
}

func assertStandardTxJSON(t *testing.T, txConfig client.TxConfig, tx sdk.Tx) {
	t.Helper()

	encoded, err := txConfig.TxJSONEncoder()(tx)
	if err != nil {
		t.Fatalf("JSON encode decoded tx: %v", err)
	}
	if !json.Valid(encoded) {
		t.Fatalf("standard TxConfig returned invalid JSON: %s", encoded)
	}
}

func encodeTestAddress(t *testing.T, bz []byte) string {
	t.Helper()
	address, err := sdkbech32.ConvertAndEncode(Bech32PrefixAccAddr, bz)
	if err != nil {
		t.Fatalf("encode account address: %v", err)
	}
	return address
}

func configureSDKBech32ForTest() {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
}
