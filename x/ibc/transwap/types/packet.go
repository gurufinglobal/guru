package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	ibcerrors "github.com/cosmos/ibc-go/v10/modules/core/errors"
	"google.golang.org/protobuf/encoding/protowire"

	errorsmod "cosmossdk.io/errors"
)

// InternalTransferRepresentation defines a struct used internally by the transfer application.
type InternalTransferRepresentation struct {
	ExchangeID string
	Token      *Token
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

func NewFungibleTokenPacketData(denom string, amount string, sender, receiver string, memo string) *FungibleTokenPacketData {
	return &FungibleTokenPacketData{
		ExchangeId: "0",
		Denom:      denom,
		Amount:     amount,
		Sender:     sender,
		Receiver:   receiver,
		Memo:       memo,
	}
}

func ValidateFungibleTokenPacketData(ftpd *FungibleTokenPacketData) error {
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

func FungibleTokenPacketDataBytes(ftpd *FungibleTokenPacketData) []byte {
	bz, err := json.Marshal(ftpd)
	if err != nil {
		panic(errors.New("cannot marshal FungibleTokenPacketData into bytes"))
	}
	return bz
}

func FungibleTokenPacketDataCustomPacketData(ftpd *FungibleTokenPacketData, key string) any {
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

func UnmarshalFungibleTokenPacketDataJSON(bz []byte, ftpd *FungibleTokenPacketData) error {
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
	var decoded FungibleTokenPacketData
	if err := json.Unmarshal(bz, &decoded); err != nil {
		return err
	}
	*ftpd = decoded
	return nil
}

func NewInternalTransferRepresentation(exchangeID string, token *Token, sender, receiver string, memo string) InternalTransferRepresentation {
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

func MarshalPacketData(data *FungibleTokenPacketData, ics20Version string, encoding string) ([]byte, error) {
	if ics20Version != V1 {
		panic("unsupported ics20 version")
	}

	switch encoding {
	case EncodingJSON:
		return json.Marshal(data)
	case EncodingProtobuf:
		if data == nil {
			return nil, errorsmod.Wrap(ibcerrors.ErrInvalidType, "packet data cannot be nil")
		}
		return data.Marshal()
	case EncodingABI:
		return EncodeABIFungibleTokenPacketData(data)
	default:
		return nil, errorsmod.Wrapf(ibcerrors.ErrInvalidType, "invalid encoding provided, must be either empty or one of [%q, %q], got %s", EncodingJSON, EncodingProtobuf, encoding)
	}
}

func UnmarshalPacketData(bz []byte, ics20Version string, encoding string) (InternalTransferRepresentation, error) {
	const failedUnmarshalingErrorMsg = "cannot unmarshal %s transfer packet data: %s"

	data := &FungibleTokenPacketData{}
	switch ics20Version {
	case V1:
		if encoding == "" {
			encoding = EncodingJSON
		}
	default:
		return InternalTransferRepresentation{}, errorsmod.Wrap(ErrInvalidVersion, ics20Version)
	}

	errorMsgVersion := "ICS20-V1"

	switch encoding {
	case EncodingJSON:
		if err := UnmarshalFungibleTokenPacketDataJSON(bz, data); err != nil {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, err.Error())
		}
	case EncodingProtobuf:
		if err := rejectUnknownFungibleTokenPacketFields(bz); err != nil {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, err.Error())
		}
		if err := data.Unmarshal(bz); err != nil {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, err.Error())
		}
	case EncodingABI:
		decoded, err := DecodeABIFungibleTokenPacketData(bz)
		if err != nil {
			return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, failedUnmarshalingErrorMsg, errorMsgVersion, err.Error())
		}
		data = decoded
	default:
		return InternalTransferRepresentation{}, errorsmod.Wrapf(ibcerrors.ErrInvalidType, "invalid encoding provided, must be either empty or one of [%q, %q, %q], got %s", EncodingJSON, EncodingProtobuf, EncodingABI, encoding)
	}

	return PacketDataV1ToInternal(data)
}

// rejectUnknownFungibleTokenPacketFields preserves the strict protobuf packet
// boundary previously provided by protov2 unknown-field reflection. Generated
// gogo unmarshallers otherwise skip and discard unknown fields silently.
func rejectUnknownFungibleTokenPacketFields(bz []byte) error {
	for len(bz) > 0 {
		fieldNumber, wireType, tagLength := protowire.ConsumeTag(bz)
		if tagLength < 0 {
			return protowire.ParseError(tagLength)
		}
		if fieldNumber < 1 || fieldNumber > 6 {
			return fmt.Errorf("unknown protobuf packet field %d", fieldNumber)
		}

		valueLength := protowire.ConsumeFieldValue(fieldNumber, wireType, bz[tagLength:])
		if valueLength < 0 {
			return protowire.ParseError(valueLength)
		}
		bz = bz[tagLength+valueLength:]
	}
	return nil
}

func PacketDataV1ToInternal(packetData *FungibleTokenPacketData) (InternalTransferRepresentation, error) {
	if err := ValidateFungibleTokenPacketData(packetData); err != nil {
		return InternalTransferRepresentation{}, errorsmod.Wrapf(err, "invalid packet data")
	}

	return InternalTransferRepresentation{
		ExchangeID: packetData.ExchangeId,
		Token: &Token{
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
