package params

import (
	"fmt"
	"time"

	txv1beta1 "cosmossdk.io/api/cosmos/tx/v1beta1"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/protobuf/proto"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
)

// pulsarDecodedTx preserves the original tx bytes for re-encoding while
// exposing SDK tx interfaces after the Guru Pulsar fallback decoder succeeds.
type pulsarDecodedTx struct {
	cdc        codec.Codec
	tx         *txtypes.Tx
	rawTxBytes []byte

	bodyBytes                  []byte
	authInfoBytes              []byte
	apiBody                    *txv1beta1.TxBody
	apiAuthInfo                *txv1beta1.AuthInfo
	bodyHasUnknownNonCriticals bool

	signers [][]byte
	msgsV2  []proto.Message
}

func (tx *pulsarDecodedTx) GetMsgs() []sdk.Msg {
	return tx.tx.GetMsgs()
}

func (tx *pulsarDecodedTx) GetMsgsV2() ([]proto.Message, error) {
	if tx.msgsV2 == nil {
		if err := tx.initSignersAndMsgsV2(); err != nil {
			return nil, err
		}
	}
	return tx.msgsV2, nil
}

func (tx *pulsarDecodedTx) ValidateBasic() error {
	if tx.tx == nil {
		return fmt.Errorf("bad Tx")
	}

	if err := tx.tx.ValidateBasic(); err != nil {
		return err
	}

	signers, err := tx.GetSigners()
	if err != nil {
		return err
	}
	if len(tx.tx.Signatures) != len(signers) {
		return errorsmod.Wrapf(
			sdkerrors.ErrUnauthorized,
			"wrong number of signers; expected %d, got %d",
			len(signers),
			len(tx.tx.Signatures),
		)
	}

	return nil
}

func (tx *pulsarDecodedTx) GetSigningTxData() txsigning.TxData {
	return txsigning.TxData{
		Body:                       tx.apiBody,
		AuthInfo:                   tx.apiAuthInfo,
		BodyBytes:                  tx.bodyBytes,
		AuthInfoBytes:              tx.authInfoBytes,
		BodyHasUnknownNonCriticals: tx.bodyHasUnknownNonCriticals,
	}
}

func (tx *pulsarDecodedTx) initSignersAndMsgsV2() error {
	signers, msgsV2, err := tx.tx.GetSigners(tx.cdc)
	if err != nil {
		return err
	}
	tx.signers = signers
	tx.msgsV2 = msgsV2
	return nil
}

func (tx *pulsarDecodedTx) GetSigners() ([][]byte, error) {
	if tx.signers == nil {
		if err := tx.initSignersAndMsgsV2(); err != nil {
			return nil, err
		}
	}
	return tx.signers, nil
}

func (tx *pulsarDecodedTx) GetPubKeys() ([]cryptotypes.PubKey, error) {
	signerInfos := tx.tx.AuthInfo.SignerInfos
	pks := make([]cryptotypes.PubKey, len(signerInfos))

	for i, si := range signerInfos {
		if si.PublicKey == nil {
			continue
		}
		pkAny := si.PublicKey.GetCachedValue()
		pk, ok := pkAny.(cryptotypes.PubKey)
		if !ok {
			return nil, errorsmod.Wrapf(sdkerrors.ErrLogic, "expecting PubKey, got: %T", pkAny)
		}
		pks[i] = pk
	}

	return pks, nil
}

func (tx *pulsarDecodedTx) GetGas() uint64 {
	if tx.tx.AuthInfo.Fee == nil {
		return 0
	}
	return tx.tx.AuthInfo.Fee.GasLimit
}

func (tx *pulsarDecodedTx) GetFee() sdk.Coins {
	if tx.tx.AuthInfo.Fee == nil {
		return nil
	}
	return tx.tx.AuthInfo.Fee.Amount
}

func (tx *pulsarDecodedTx) FeePayer() []byte {
	if tx.tx.AuthInfo.Fee != nil && tx.tx.AuthInfo.Fee.Payer != "" {
		feePayer, err := tx.cdc.InterfaceRegistry().SigningContext().AddressCodec().StringToBytes(tx.tx.AuthInfo.Fee.Payer)
		if err != nil {
			panic(err)
		}
		return feePayer
	}

	signers, err := tx.GetSigners()
	if err != nil || len(signers) == 0 {
		return nil
	}
	return signers[0]
}

func (tx *pulsarDecodedTx) FeeGranter() []byte {
	if tx.tx.AuthInfo.Fee == nil || tx.tx.AuthInfo.Fee.Granter == "" {
		return nil
	}
	feeGranter, err := tx.cdc.InterfaceRegistry().SigningContext().AddressCodec().StringToBytes(tx.tx.AuthInfo.Fee.Granter)
	if err != nil {
		panic(err)
	}
	return feeGranter
}

func (tx *pulsarDecodedTx) GetMemo() string {
	return tx.tx.Body.Memo
}

func (tx *pulsarDecodedTx) GetTimeoutHeight() uint64 {
	return tx.tx.Body.TimeoutHeight
}

func (tx *pulsarDecodedTx) GetTimeoutTimeStamp() time.Time {
	timestamp := tx.tx.Body.TimeoutTimestamp
	if timestamp == nil {
		return time.Time{}
	}
	return *timestamp
}

func (tx *pulsarDecodedTx) GetUnordered() bool {
	return tx.tx.Body.Unordered
}

func (tx *pulsarDecodedTx) GetExtensionOptions() []*codectypes.Any {
	return tx.tx.Body.ExtensionOptions
}

func (tx *pulsarDecodedTx) GetNonCriticalExtensionOptions() []*codectypes.Any {
	return tx.tx.Body.NonCriticalExtensionOptions
}

func (tx *pulsarDecodedTx) GetSignaturesV2() ([]signingtypes.SignatureV2, error) {
	signerInfos := tx.tx.AuthInfo.SignerInfos
	sigs := tx.tx.Signatures
	if len(sigs) < len(signerInfos) {
		return nil, fmt.Errorf("signature count %d is less than signer info count %d", len(sigs), len(signerInfos))
	}

	pubKeys, err := tx.GetPubKeys()
	if err != nil {
		return nil, err
	}

	res := make([]signingtypes.SignatureV2, len(signerInfos))
	for i, signerInfo := range signerInfos {
		if signerInfo.ModeInfo == nil {
			res[i] = signingtypes.SignatureV2{PubKey: pubKeys[i]}
			continue
		}

		sigData, err := authtx.ModeInfoAndSigToSignatureData(signerInfo.ModeInfo, sigs[i])
		if err != nil {
			return nil, err
		}
		res[i] = signingtypes.SignatureV2{
			PubKey:   pubKeys[i],
			Data:     sigData,
			Sequence: signerInfo.GetSequence(),
		}
	}

	return res, nil
}
