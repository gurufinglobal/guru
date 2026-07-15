package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	ibcerrors "github.com/cosmos/ibc-go/v11/modules/core/errors"
	"google.golang.org/protobuf/proto"

	errorsmod "cosmossdk.io/errors"
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

// PacketKind identifies whether a transwap packet requests a plain transfer or
// an exchange. The transwap port deliberately uses exchange_id = "0" as its
// plain-transfer sentinel and canonical positive uint64 values for exchanges.
type PacketKind uint8

const (
	PacketKindUnspecified PacketKind = iota
	PacketKindTransfer
	PacketKindExchange
)

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

func ValidateFungibleTokenPacketData(ftpd *transwapv1.FungibleTokenPacketData) error {
	if ftpd == nil {
		return errorsmod.Wrap(ibcerrors.ErrInvalidType, "packet data cannot be nil")
	}
	if _, err := classifyExchangeID(ftpd.ExchangeId); err != nil {
		return err
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
	if ftpd == nil {
		return fmt.Errorf("packet data target cannot be nil")
	}

	fields, duplicateFields, err := decodeJSONObject(bz)
	if err != nil {
		return err
	}
	if len(duplicateFields) > 0 {
		return fmt.Errorf("duplicate packet field %q", duplicateFields[0])
	}

	unknownFields := make([]string, 0)
	for field := range fields {
		switch field {
		case "exchange_id", "denom", "amount", "sender", "receiver", "memo":
		default:
			unknownFields = append(unknownFields, field)
		}
	}
	if len(unknownFields) > 0 {
		sort.Strings(unknownFields)
		return fmt.Errorf("unknown packet field %q", unknownFields[0])
	}

	// Decode into a temporary value so a malformed packet cannot partially
	// mutate a caller-owned message before returning an error.
	var decoded transwapv1.FungibleTokenPacketData
	if err := json.Unmarshal(bz, &decoded); err != nil {
		return err
	}
	proto.Reset(ftpd)
	proto.Merge(ftpd, &decoded)
	return nil
}

func NewInternalTransferRepresentation(exchangeID string, token *transwapv1.Token, sender, receiver string, memo string) InternalTransferRepresentation {
	return InternalTransferRepresentation{ExchangeID: exchangeID, Token: token, Sender: sender, Receiver: receiver, Memo: memo}
}

func (ftpd InternalTransferRepresentation) ValidateBasic() error {
	if _, err := ftpd.ClassifyPacket(); err != nil {
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
	kind, err := ftpd.ClassifyPacket()
	return err == nil && kind == PacketKindTransfer
}

// ClassifyPacket validates the custom transwap exchange_id discriminator and
// returns the corresponding packet kind. Non-canonical encodings are rejected
// so every implementation observes the same packet semantics.
func (ftpd InternalTransferRepresentation) ClassifyPacket() (PacketKind, error) {
	kind, _, err := ClassifyExchangeID(ftpd.ExchangeID)
	return kind, err
}

func classifyExchangeID(raw string) (PacketKind, error) {
	kind, _, err := ClassifyExchangeID(raw)
	return kind, err
}

// ClassifyExchangeID validates a raw exchange_id and returns both its packet
// kind and numeric exchange ID. The numeric ID is zero only for transfers.
func ClassifyExchangeID(raw string) (PacketKind, uint64, error) {
	if raw == "0" {
		return PacketKindTransfer, 0, nil
	}

	exchangeID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || exchangeID == 0 || strconv.FormatUint(exchangeID, 10) != raw {
		return PacketKindUnspecified, 0, errorsmod.Wrapf(ErrInvalidExchangeID, "exchange_id must be \"0\" or a canonical positive uint64: %q", raw)
	}

	return PacketKindExchange, exchangeID, nil
}
