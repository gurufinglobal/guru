package params

import (
	"strings"

	txv1beta1 "cosmossdk.io/api/cosmos/tx/v1beta1"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/unknownproto"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txdecode "github.com/cosmos/cosmos-sdk/x/tx/decode"
)

func newPulsarTxDecoder(cdc codec.Codec) (sdk.TxDecoder, error) {
	defaultDecoder := authtx.DefaultTxDecoder(cdc)

	return func(txBytes []byte) (sdk.Tx, error) {
		tx, defaultErr := defaultDecoder(txBytes)
		if defaultErr == nil {
			return tx, nil
		}
		if !shouldUsePulsarFallback(defaultErr) {
			return nil, defaultErr
		}

		var raw txtypes.TxRaw
		if err := cdc.Unmarshal(txBytes, &raw); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		var apiBody txv1beta1.TxBody
		bodyHasUnknownNonCriticals, err := txdecode.RejectUnknownFields(
			raw.BodyBytes,
			apiBody.ProtoReflect().Descriptor(),
			true,
			protoregistry.GlobalFiles,
		)
		if err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}
		if err := proto.Unmarshal(raw.BodyBytes, &apiBody); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		var body txtypes.TxBody
		if err := cdc.Unmarshal(raw.BodyBytes, &body); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		var authInfo txtypes.AuthInfo
		if err := unknownproto.RejectUnknownFieldsStrict(raw.AuthInfoBytes, &authInfo, cdc.InterfaceRegistry()); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}
		if err := cdc.Unmarshal(raw.AuthInfoBytes, &authInfo); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		var apiAuthInfo txv1beta1.AuthInfo
		if err := proto.Unmarshal(raw.AuthInfoBytes, &apiAuthInfo); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		return &pulsarDecodedTx{
			cdc:                        cdc,
			tx:                         &txtypes.Tx{Body: &body, AuthInfo: &authInfo, Signatures: raw.Signatures},
			rawTxBytes:                 append([]byte(nil), txBytes...),
			bodyBytes:                  append([]byte(nil), raw.BodyBytes...),
			authInfoBytes:              append([]byte(nil), raw.AuthInfoBytes...),
			apiBody:                    &apiBody,
			apiAuthInfo:                &apiAuthInfo,
			bodyHasUnknownNonCriticals: bodyHasUnknownNonCriticals,
		}, nil
	}, nil
}

func shouldUsePulsarFallback(err error) bool {
	msg := err.Error()
	// Keep the fallback scoped to Guru Pulsar nested messages. SDK/gogo txs
	// must keep using the default x/auth/tx decoder.
	return strings.Contains(msg, `failed to retrieve the message of type "guru.`)
}

func newPulsarTxEncoder(base sdk.TxEncoder) sdk.TxEncoder {
	return func(tx sdk.Tx) ([]byte, error) {
		if decoded, ok := tx.(*pulsarDecodedTx); ok {
			return append([]byte(nil), decoded.rawTxBytes...), nil
		}
		return base(tx)
	}
}
