package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	ibcerrors "github.com/cosmos/ibc-go/v11/modules/core/errors"
	"google.golang.org/protobuf/proto"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
)

// InternalTransferRepresentation defines a struct used internally by the transfer application.
type InternalTransferRepresentation struct {
	ExchangeID string
	Token      *transwapv1.Token
	Sender     string
	Receiver   string
	Memo       string
}

const (
	EncodingJSON     = "application/json"
	EncodingProtobuf = "application/x-protobuf"
	EncodingABI      = "application/x-solidity-abi"
)

func NewFungibleTokenPacketData(denom string, amount string, sender, receiver string, memo string) *transwapv1.FungibleTokenPacketData {
	return &transwapv1.FungibleTokenPacketData{
		ExchangeId: "0",
		Denom:      denom,
		Amount:     amount,
		Sender:     sender,
		Receiver:   receiver,
		Memo:       memo,
	}
}

func NewTransferPacketData(sourcePort string, sourceChannel string, token *transwapv1.Token, sender, receiver string, memo string, timeoutTimestamp uint64, fee sdk.Coin, exchangeID string) *transwapv1.TransferPacketData {
	packet := &transwapv1.TransferPacketData{
		SourcePort:               sourcePort,
		SourceChannel:            sourceChannel,
		Token:                    token,
		Sender:                   sender,
		Receiver:                 receiver,
		Memo:                     memo,
		TimeoutTimestamp:         timeoutTimestamp,
		Fee:                      SDKCoinToProto(fee),
		ExchangeId:               exchangeID,
		OriginalTimeoutTimestamp: timeoutTimestamp,
	}
	coin, err := TokenToCoin(token)
	if err == nil && coin.Denom == fee.Denom && !fee.Amount.IsNegative() && coin.Amount.GT(fee.Amount) {
		packet.PendingLiability = SDKCoinToProto(sdk.NewCoin(coin.Denom, coin.Amount.Sub(fee.Amount)))
	}
	return packet
}

func ValidateFungibleTokenPacketData(ftpd *transwapv1.FungibleTokenPacketData) error {
	if ftpd == nil {
		return errorsmod.Wrap(ibcerrors.ErrInvalidType, "packet data cannot be nil")
	}
	if err := validateAmount(ftpd.Amount); err != nil {
		return err
	}
	if strings.TrimSpace(ftpd.Sender) == "" {
		return errorsmod.Wrap(ibcerrors.ErrInvalidAddress, "sender address cannot be blank")
	}
	if strings.TrimSpace(ftpd.Receiver) == "" {
		return errorsmod.Wrap(ibcerrors.ErrInvalidAddress, "receiver address cannot be blank")
	}
	if len(ftpd.Receiver) > MaximumReceiverLength {
		return errorsmod.Wrapf(ibcerrors.ErrInvalidAddress, "receiver address must not exceed %d bytes", MaximumReceiverLength)
	}
	if len(ftpd.Memo) > MaximumMemoLength {
		return errorsmod.Wrapf(ErrInvalidMemo, "memo must not exceed %d bytes", MaximumMemoLength)
	}
	return ValidateDenom(ExtractDenomFromPath(ftpd.Denom))
}

func FungibleTokenPacketDataBytes(ftpd *transwapv1.FungibleTokenPacketData) []byte {
	bz, err := json.Marshal(ftpd)
	if err != nil {
		panic(errors.New("cannot marshal FungibleTokenPacketData into bytes"))
	}
	return bz
}

func FungibleTokenPacketDataCustomPacketData(ftpd *transwapv1.FungibleTokenPacketData, key string) any {
	if ftpd == nil {
		return nil
	}
	if len(ftpd.Memo) == 0 {
		return nil
	}

	jsonObject := make(map[string]any)
	if err := json.Unmarshal([]byte(ftpd.Memo), &jsonObject); err != nil {
		return nil
	}

	memoData, found := jsonObject[key]
	if !found {
		return nil
	}

	return memoData
}

func UnmarshalFungibleTokenPacketDataJSON(bz []byte, ftpd *transwapv1.FungibleTokenPacketData) error {
	d := json.NewDecoder(bytes.NewReader(bz))
	d.DisallowUnknownFields()
	if err := d.Decode(ftpd); err != nil {
		return err
	}
	return nil
}

func NewInternalTransferRepresentation(exchangeID string, token *transwapv1.Token, sender, receiver string, memo string) InternalTransferRepresentation {
	return InternalTransferRepresentation{ExchangeID: exchangeID, Token: token, Sender: sender, Receiver: receiver, Memo: memo}
}

func (ftpd InternalTransferRepresentation) ValidateBasic() error {
	_, ok := sdkmath.NewIntFromString(ftpd.ExchangeID)
	if !ok {
		return errorsmod.Wrapf(ErrInvalidAmount, "unable to parse exchange id: %s", ftpd.ExchangeID)
	}

	if strings.TrimSpace(ftpd.Sender) == "" {
		return errorsmod.Wrap(ibcerrors.ErrInvalidAddress, "sender address cannot be blank")
	}

	if strings.TrimSpace(ftpd.Receiver) == "" {
		return errorsmod.Wrap(ibcerrors.ErrInvalidAddress, "receiver address cannot be blank")
	}

	if len(ftpd.Receiver) > MaximumReceiverLength {
		return errorsmod.Wrapf(ibcerrors.ErrInvalidAddress, "receiver address must not exceed %d bytes", MaximumReceiverLength)
	}

	if err := ValidateToken(ftpd.Token); err != nil {
		return err
	}

	if len(ftpd.Memo) > MaximumMemoLength {
		return errorsmod.Wrapf(ErrInvalidMemo, "memo must not exceed %d bytes", MaximumMemoLength)
	}

	return nil
}

func (ftpd InternalTransferRepresentation) GetCustomPacketData(key string) any {
	if len(ftpd.Memo) == 0 {
		return nil
	}

	jsonObject := make(map[string]any)
	if err := json.Unmarshal([]byte(ftpd.Memo), &jsonObject); err != nil {
		return nil
	}

	memoData, found := jsonObject[key]
	if !found {
		return nil
	}

	return memoData
}

func (ftpd InternalTransferRepresentation) GetPacketSender(sourcePortID string) string {
	return ftpd.Sender
}

func MarshalPacketData(data *transwapv1.FungibleTokenPacketData, ics20Version string, encoding string) ([]byte, error) {
	if ics20Version != V1 {
		panic("unsupported ics20 version")
	}

	switch encoding {
	case EncodingJSON:
		return json.Marshal(data)
	case EncodingProtobuf:
		return proto.Marshal(data)
	case EncodingABI:
		return EncodeABIFungibleTokenPacketData(data)
	default:
		return nil, errorsmod.Wrapf(ibcerrors.ErrInvalidType, "invalid encoding provided, must be either empty or one of [%q, %q], got %s", EncodingJSON, EncodingProtobuf, encoding)
	}
}

func UnmarshalPacketData(bz []byte, ics20Version string, encoding string) (InternalTransferRepresentation, error) {
	const failedUnmarshalingErrorMsg = "cannot unmarshal %s transfer packet data: %s"

	var data proto.Message
	switch ics20Version {
	case V1:
		if encoding == "" {
			encoding = EncodingJSON
		}
		data = &transwapv1.FungibleTokenPacketData{}
	default:
		return InternalTransferRepresentation{}, errorsmod.Wrap(ErrInvalidVersion, ics20Version)
	}

	errorMsgVersion := "ICS20-V1"

	switch encoding {
	case EncodingJSON:
		if err := UnmarshalFungibleTokenPacketDataJSON(bz, data.(*transwapv1.FungibleTokenPacketData)); err != nil {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, err.Error())
		}
	case EncodingProtobuf:
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(bz, data); err != nil {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, err.Error())
		}
		if len(data.ProtoReflect().GetUnknown()) != 0 {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, "unknown protobuf fields")
		}
	case EncodingABI:
		var err error
		data, err = DecodeABIFungibleTokenPacketData(bz)
		if err != nil {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, err.Error())
		}
	default:
		return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, "invalid encoding provided, must be either empty or one of [%q, %q, %q], got %s", EncodingJSON, EncodingProtobuf, EncodingABI, encoding)
	}

	datav1, ok := data.(*transwapv1.FungibleTokenPacketData)
	if !ok {
		return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, "cannot convert proto message into FungibleTokenPacketData")
	}

	return PacketDataV1ToInternal(datav1)
}

func PacketDataV1ToInternal(packetData *transwapv1.FungibleTokenPacketData) (InternalTransferRepresentation, error) {
	if err := ValidateFungibleTokenPacketData(packetData); err != nil {
		return InternalTransferRepresentation{}, errorsmod.Wrapf(err, "invalid packet data")
	}

	return InternalTransferRepresentation{
		ExchangeID: packetData.ExchangeId,
		Token: &transwapv1.Token{
			Denom:  ExtractDenomFromPath(packetData.Denom),
			Amount: packetData.Amount,
		},
		Sender:   packetData.Sender,
		Receiver: packetData.Receiver,
		Memo:     packetData.Memo,
	}, nil
}

func (ftpd InternalTransferRepresentation) IsTransferPacket() bool {
	return ftpd.ExchangeID == "0"
}
