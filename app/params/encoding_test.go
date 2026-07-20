package params_test

import (
	"context"
	"testing"
	"time"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	txv1beta1 "cosmossdk.io/api/cosmos/tx/v1beta1"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
	evmantetypes "github.com/cosmos/evm/ante/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestStandardTxConfigDecodesMixedInternalMessages(t *testing.T) {
	config := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	banktypes.RegisterInterfaces(config.InterfaceRegistry)
	oracletypes.RegisterInterfaces(config.InterfaceRegistry)
	constitutiontypes.RegisterInterfaces(config.InterfaceRegistry)

	require.Equal(t, []signingv1beta1.SignMode{
		signingv1beta1.SignMode_SIGN_MODE_DIRECT,
		signingv1beta1.SignMode_SIGN_MODE_DIRECT_AUX,
	}, config.TxConfig.SignModeHandler().SupportedModes())
	require.Equal(t, signingv1beta1.SignMode_SIGN_MODE_DIRECT, config.TxConfig.SignModeHandler().DefaultMode())

	addressCodec := config.InterfaceRegistry.SigningContext().AddressCodec()
	sender, err := addressCodec.BytesToString(bytesOf(0x01, 20))
	require.NoError(t, err)
	receiver, err := addressCodec.BytesToString(bytesOf(0x02, 20))
	require.NoError(t, err)

	messages := []sdk.Msg{
		&banktypes.MsgSend{
			FromAddress: sender,
			ToAddress:   receiver,
			Amount:      sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
		},
		&oracletypes.MsgUpsertTask{
			Moderator: sender,
			Task: &oracletypes.OracleTask{
				Symbol:             "BTC/USD",
				ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 5,
			},
		},
		&constitutiontypes.MsgUpdateSeparationRatio{
			Moderator: sender,
			SeparationRatio: &constitutiontypes.SeparationRatio{
				BasePpm:       100_000,
				BurnPpm:       200_000,
				ValidatorsPpm: 700_000,
			},
		},
	}

	builder := config.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(messages...))
	txBytes, err := config.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)

	decoded, err := config.TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	require.Len(t, decoded.GetMsgs(), len(messages))
	require.IsType(t, &banktypes.MsgSend{}, decoded.GetMsgs()[0])
	oracleMsg := decoded.GetMsgs()[1].(*oracletypes.MsgUpsertTask)
	require.Equal(t, "BTC/USD", oracleMsg.GetTask().GetSymbol())
	constitutionMsg := decoded.GetMsgs()[2].(*constitutiontypes.MsgUpdateSeparationRatio)
	require.Equal(t, uint32(700_000), constitutionMsg.GetSeparationRatio().GetValidatorsPpm())

	sigTx, ok := decoded.(authsigning.SigVerifiableTx)
	require.True(t, ok)
	signers, err := sigTx.GetSigners()
	require.NoError(t, err)
	require.Len(t, signers, 1)
	require.Equal(t, bytesOf(0x01, 20), []byte(signers[0]))
	_, err = decoded.GetMsgsV2()
	require.NoError(t, err)

	anyTx, ok := decoded.(interface{ AsAny() *codectypes.Any })
	require.True(t, ok)
	require.Equal(t, "/cosmos.tx.v1beta1.Tx", anyTx.AsAny().GetTypeUrl())

	wrapped, err := config.TxConfig.WrapTxBuilder(decoded)
	require.NoError(t, err)
	reencoded, err := config.TxConfig.TxEncoder()(wrapped.GetTx())
	require.NoError(t, err)
	require.Equal(t, txBytes, reencoded)
}

func TestStandardTxConfigPreservesNestedMessagesAndMetadata(t *testing.T) {
	sdkConfig := sdk.GetConfig()
	config := appparams.MakeEncodingConfig(
		sdkConfig.GetBech32AccountAddrPrefix(),
		sdkConfig.GetBech32ValidatorAddrPrefix(),
		sdkConfig.GetBech32ConsensusAddrPrefix(),
	)
	authz.RegisterInterfaces(config.InterfaceRegistry)
	feegrant.RegisterInterfaces(config.InterfaceRegistry)
	oracletypes.RegisterInterfaces(config.InterfaceRegistry)

	actor := sdk.AccAddress(bytesOf(0x11, 20))
	feePayer := sdk.AccAddress(bytesOf(0x22, 20))
	feeGranter := sdk.AccAddress(bytesOf(0x33, 20))
	nested := &oracletypes.MsgUpsertTask{
		Moderator: actor.String(),
		Task: &oracletypes.OracleTask{
			Symbol:             "ETH/USD",
			ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 7,
		},
	}
	exec := authz.NewMsgExec(actor, []sdk.Msg{nested})
	grant, err := feegrant.NewMsgGrantAllowance(
		&feegrant.BasicAllowance{SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 50))},
		actor,
		actor,
	)
	require.NoError(t, err)

	extension, err := codectypes.NewAnyWithValue(&evmantetypes.ExtensionOptionDynamicFeeTx{})
	require.NoError(t, err)
	require.Equal(t, "/cosmos.evm.ante.v1.ExtensionOptionDynamicFeeTx", extension.GetTypeUrl())

	timeout := time.Unix(1_700_000_000, 123)
	builder := config.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&exec, grant))
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 3)))
	builder.SetGasLimit(200_000)
	builder.SetFeePayer(feePayer)
	builder.SetFeeGranter(feeGranter)
	builder.SetTimeoutHeight(1234)
	builder.SetTimeoutTimestamp(timeout)
	builder.SetUnordered(true)
	extensionBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder)
	require.True(t, ok)
	extensionBuilder.SetExtensionOptions(extension)
	extensionBuilder.SetNonCriticalExtensionOptions(extension)

	txBytes, err := config.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	decoded, err := config.TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	require.Len(t, decoded.GetMsgs(), 2)

	decodedExec, ok := decoded.GetMsgs()[0].(*authz.MsgExec)
	require.True(t, ok)
	nestedMessages, err := decodedExec.GetMessages()
	require.NoError(t, err)
	require.Len(t, nestedMessages, 1)
	decodedNested, ok := nestedMessages[0].(*oracletypes.MsgUpsertTask)
	require.True(t, ok)
	require.Equal(t, "ETH/USD", decodedNested.GetTask().GetSymbol())
	require.Equal(t, uint32(7), decodedNested.GetTask().GetSubmissionInterval())

	decodedGrant, ok := decoded.GetMsgs()[1].(*feegrant.MsgGrantAllowance)
	require.True(t, ok)
	allowance, err := decodedGrant.GetFeeAllowanceI()
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 50)), allowance.(*feegrant.BasicAllowance).GetSpendLimit())

	feeTx, ok := decoded.(sdk.FeeTx)
	require.True(t, ok)
	require.Equal(t, feePayer, sdk.AccAddress(feeTx.FeePayer()))
	require.Equal(t, feeGranter, sdk.AccAddress(feeTx.FeeGranter()))
	require.Equal(t, uint64(200_000), feeTx.GetGas())

	sigTx, ok := decoded.(authsigning.SigVerifiableTx)
	require.True(t, ok)
	signers, err := sigTx.GetSigners()
	require.NoError(t, err)
	require.Equal(t, [][]byte{actor, feePayer}, signers)

	timeoutHeightTx, ok := decoded.(sdk.TxWithTimeoutHeight)
	require.True(t, ok)
	require.Equal(t, uint64(1234), timeoutHeightTx.GetTimeoutHeight())
	unorderedTx, ok := decoded.(sdk.TxWithUnordered)
	require.True(t, ok)
	require.True(t, unorderedTx.GetUnordered())
	require.True(t, timeout.Equal(unorderedTx.GetTimeoutTimeStamp()))

	extensionTx, ok := decoded.(ante.HasExtensionOptionsTx)
	require.True(t, ok)
	require.Len(t, extensionTx.GetExtensionOptions(), 1)
	require.Len(t, extensionTx.GetNonCriticalExtensionOptions(), 1)
	require.IsType(t, &evmantetypes.ExtensionOptionDynamicFeeTx{}, extensionTx.GetExtensionOptions()[0].GetCachedValue())
	require.IsType(t, &evmantetypes.ExtensionOptionDynamicFeeTx{}, extensionTx.GetNonCriticalExtensionOptions()[0].GetCachedValue())
}

func TestStandardTxConfigProducesDirectAndDirectAuxSignBytes(t *testing.T) {
	sdkConfig := sdk.GetConfig()
	config := appparams.MakeEncodingConfig(
		sdkConfig.GetBech32AccountAddrPrefix(),
		sdkConfig.GetBech32ValidatorAddrPrefix(),
		sdkConfig.GetBech32ConsensusAddrPrefix(),
	)
	oracletypes.RegisterInterfaces(config.InterfaceRegistry)

	privKey := secp256k1.GenPrivKey()
	actor := sdk.AccAddress(privKey.PubKey().Address())
	feePayer := sdk.AccAddress(bytesOf(0x44, 20))
	builder := config.TxConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&oracletypes.MsgUpsertTask{
		Moderator: actor.String(),
		Task: &oracletypes.OracleTask{
			Symbol:             "BTC/USD",
			ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 5,
		},
	}))
	builder.SetFeePayer(feePayer)

	provider, ok := builder.GetTx().(interface{ GetSigningTxData() txsigning.TxData })
	require.True(t, ok)
	txData := provider.GetSigningTxData()
	pubKey, err := codectypes.NewAnyWithValue(privKey.PubKey())
	require.NoError(t, err)
	signerData := txsigning.SignerData{
		Address:       actor.String(),
		ChainID:       "guru_631",
		AccountNumber: 9,
		Sequence:      10,
		PubKey:        &anypb.Any{TypeUrl: pubKey.TypeUrl, Value: pubKey.Value},
	}

	directBytes, err := config.TxConfig.SignModeHandler().GetSignBytes(
		context.Background(), signingv1beta1.SignMode_SIGN_MODE_DIRECT, signerData, txData,
	)
	require.NoError(t, err)
	var directDoc txv1beta1.SignDoc
	require.NoError(t, proto.Unmarshal(directBytes, &directDoc))
	require.Equal(t, "guru_631", directDoc.GetChainId())
	require.Equal(t, uint64(9), directDoc.GetAccountNumber())
	require.Equal(t, txData.BodyBytes, directDoc.GetBodyBytes())
	require.Equal(t, txData.AuthInfoBytes, directDoc.GetAuthInfoBytes())

	directAuxBytes, err := config.TxConfig.SignModeHandler().GetSignBytes(
		context.Background(), signingv1beta1.SignMode_SIGN_MODE_DIRECT_AUX, signerData, txData,
	)
	require.NoError(t, err)
	var directAuxDoc txv1beta1.SignDocDirectAux
	require.NoError(t, proto.Unmarshal(directAuxBytes, &directAuxDoc))
	require.Equal(t, "guru_631", directAuxDoc.GetChainId())
	require.Equal(t, uint64(9), directAuxDoc.GetAccountNumber())
	require.Equal(t, uint64(10), directAuxDoc.GetSequence())
	require.Equal(t, txData.BodyBytes, directAuxDoc.GetBodyBytes())
	require.True(t, proto.Equal(signerData.PubKey, directAuxDoc.GetPublicKey()))
}

func TestStandardTxConfigRejectsNestedUnknownFieldsAndNonADR027TxRaw(t *testing.T) {
	config := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	oracletypes.RegisterInterfaces(config.InterfaceRegistry)

	msg := &oracletypes.MsgUpsertTask{
		Moderator: "guru1moderator",
		Task: &oracletypes.OracleTask{
			Symbol:             "BTC/USD",
			ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 5,
		},
	}
	msgBytes, err := msg.Marshal()
	require.NoError(t, err)
	badMsgBytes := protowire.AppendTag(append([]byte(nil), msgBytes...), 100, protowire.VarintType)
	badMsgBytes = protowire.AppendVarint(badMsgBytes, 1)

	encodeRaw := func(messageBytes []byte) []byte {
		body := &txtypes.TxBody{Messages: []*codectypes.Any{{
			TypeUrl: "/guru.oracle.v1.MsgUpsertTask",
			Value:   messageBytes,
		}}}
		bodyBytes, marshalErr := body.Marshal()
		require.NoError(t, marshalErr)
		authInfoBytes, marshalErr := (&txtypes.AuthInfo{Fee: &txtypes.Fee{}}).Marshal()
		require.NoError(t, marshalErr)
		rawBytes, marshalErr := (&txtypes.TxRaw{BodyBytes: bodyBytes, AuthInfoBytes: authInfoBytes}).Marshal()
		require.NoError(t, marshalErr)
		return rawBytes
	}

	_, err = config.TxConfig.TxDecoder()(encodeRaw(badMsgBytes))
	require.ErrorContains(t, err, "errUnknownField")

	validBytes := encodeRaw(msgBytes)
	_, _, firstFieldLength := protowire.ConsumeField(validBytes)
	require.Positive(t, firstFieldLength)
	nonADR027 := append(append([]byte(nil), validBytes[firstFieldLength:]...), validBytes[:firstFieldLength]...)
	_, err = config.TxConfig.TxDecoder()(nonADR027)
	require.ErrorContains(t, err, "ADR-027")
}

func bytesOf(value byte, length int) []byte {
	bz := make([]byte, length)
	for i := range bz {
		bz[i] = value
	}
	return bz
}
