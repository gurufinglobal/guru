package types

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
)

func TestValidateTokenRejectsInvalidDenomAndAmounts(t *testing.T) {
	valid := transwapv1.Token{Denom: NewDenom("ugxusd"), Amount: "1"}

	tests := []struct {
		name   string
		mutate func(*transwapv1.Token)
	}{
		{"nil denom", func(token *transwapv1.Token) { token.Denom = nil }},
		{"blank denom", func(token *transwapv1.Token) { token.Denom = NewDenom(" ") }},
		{"nil hop", func(token *transwapv1.Token) { token.Denom = NewDenom("ugxusd", nil) }},
		{"invalid hop", func(token *transwapv1.Token) { token.Denom = NewDenom("ugxusd", NewHop("bad port", "channel-0")) }},
		{"zero amount", func(token *transwapv1.Token) { token.Amount = "0" }},
		{"negative amount", func(token *transwapv1.Token) { token.Amount = "-1" }},
		{"non numeric amount", func(token *transwapv1.Token) { token.Amount = "one" }},
		{"uint256 overflow amount", func(token *transwapv1.Token) { token.Amount = newOverflowAmount() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := valid
			token.Denom = CloneDenom(valid.Denom)
			tt.mutate(&token)

			require.Error(t, ValidateToken(token))
			_, err := TokenToCoin(token)
			require.Error(t, err)
		})
	}
}

func TestFungibleTokenPacketValidationRejectsMalformedFields(t *testing.T) {
	valid := NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")

	tests := []struct {
		name   string
		mutate func(*transwapv1.FungibleTokenPacketData)
	}{
		{"blank denom", func(packet *transwapv1.FungibleTokenPacketData) { packet.Denom = " " }},
		{"invalid hop", func(packet *transwapv1.FungibleTokenPacketData) { packet.Denom = "bad port/channel-0/ugxusd" }},
		{"zero amount", func(packet *transwapv1.FungibleTokenPacketData) { packet.Amount = "0" }},
		{"negative amount", func(packet *transwapv1.FungibleTokenPacketData) { packet.Amount = "-1" }},
		{"non numeric amount", func(packet *transwapv1.FungibleTokenPacketData) { packet.Amount = "nan" }},
		{"uint256 overflow amount", func(packet *transwapv1.FungibleTokenPacketData) { packet.Amount = newOverflowAmount() }},
		{"blank sender", func(packet *transwapv1.FungibleTokenPacketData) { packet.Sender = " " }},
		{"blank receiver", func(packet *transwapv1.FungibleTokenPacketData) { packet.Receiver = " " }},
		{"receiver too long", func(packet *transwapv1.FungibleTokenPacketData) {
			packet.Receiver = strings.Repeat("r", MaximumReceiverLength+1)
		}},
		{"memo too long", func(packet *transwapv1.FungibleTokenPacketData) {
			packet.Memo = strings.Repeat("m", MaximumMemoLength+1)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := valid
			tt.mutate(&packet)

			require.Error(t, ValidateFungibleTokenPacketData(packet))
			_, err := PacketDataV1ToInternal(packet)
			require.Error(t, err)
		})
	}
}

func TestUnmarshalPacketDataRejectsJSONUnknownFields(t *testing.T) {
	packet := NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")
	bz, err := json.Marshal(map[string]string{
		"exchange_id": "0",
		"denom":       packet.Denom,
		"amount":      packet.Amount,
		"sender":      packet.Sender,
		"receiver":    packet.Receiver,
		"memo":        packet.Memo,
		"unexpected":  "field",
	})
	require.NoError(t, err)

	_, err = UnmarshalPacketData(bz, V1, EncodingJSON)
	require.Error(t, err)
}

func TestUnmarshalPacketDataRoundTripsSupportedEncodings(t *testing.T) {
	packet := NewFungibleTokenPacketData("transwap/channel-0/ugxusd", "123", "sender", "receiver", "memo")

	for _, encoding := range []string{EncodingJSON, EncodingProtobuf, EncodingABI} {
		t.Run(encoding, func(t *testing.T) {
			bz, err := MarshalPacketData(packet, V1, encoding)
			require.NoError(t, err)

			internal, err := UnmarshalPacketData(bz, V1, encoding)
			require.NoError(t, err)
			require.True(t, internal.IsTransferPacket())
			require.Equal(t, "123", internal.Token.Amount)
			require.Equal(t, "transwap/channel-0/ugxusd", DenomPath(internal.Token.Denom))
			require.Equal(t, packet.Sender, internal.Sender)
			require.Equal(t, packet.Receiver, internal.Receiver)
			require.Equal(t, packet.Memo, internal.Memo)
		})
	}
}

func TestABIPacketDataDefaultsExchangeIDToTransfer(t *testing.T) {
	packet := NewFungibleTokenPacketData("transwap/channel-0/ugxusd", "123", "sender", "receiver", "memo")
	packet.ExchangeId = "7"

	bz, err := MarshalPacketData(packet, V1, EncodingABI)
	require.NoError(t, err)

	internal, err := UnmarshalPacketData(bz, V1, EncodingABI)
	require.NoError(t, err)
	require.Equal(t, "0", internal.ExchangeID)
	require.True(t, internal.IsTransferPacket())
	require.Equal(t, "123", internal.Token.Amount)
	require.Equal(t, "transwap/channel-0/ugxusd", DenomPath(internal.Token.Denom))
	require.Equal(t, packet.Sender, internal.Sender)
	require.Equal(t, packet.Receiver, internal.Receiver)
	require.Equal(t, packet.Memo, internal.Memo)

	_, err = UnmarshalPacketData([]byte{0x01, 0x02, 0x03}, V1, EncodingABI)
	require.Error(t, err)

	packet.Amount = "not-a-number"
	_, err = MarshalPacketData(packet, V1, EncodingABI)
	require.Error(t, err)
}

func TestUnmarshalPacketDataRejectsProtobufUnknownFields(t *testing.T) {
	packet := NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")
	bz, err := proto.Marshal(&packet)
	require.NoError(t, err)

	bz = append(bz, 0x7a, 0x01, 0x00)
	_, err = UnmarshalPacketData(bz, V1, EncodingProtobuf)
	require.Error(t, err)
}

func TestInternalTransferRepresentationDistinguishesExchangePackets(t *testing.T) {
	transfer, err := PacketDataV1ToInternal(NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", ""))
	require.NoError(t, err)
	require.True(t, transfer.IsTransferPacket())

	exchangePacket := NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")
	exchangePacket.ExchangeId = "7"
	exchange, err := PacketDataV1ToInternal(exchangePacket)
	require.NoError(t, err)
	require.False(t, exchange.IsTransferPacket())

	nonNumeric := NewInternalTransferRepresentation("abc", transwapv1.Token{Denom: NewDenom("ugxusd"), Amount: "1"}, "sender", "receiver", "")
	require.Error(t, nonNumeric.ValidateBasic())
}

func TestCloneDenomAndTokenPreventTraceAliasing(t *testing.T) {
	original := NewDenom("ugxusd", NewHop("transwap", "channel-0"), NewHop("transfer", "channel-1"))
	clone := CloneDenom(original)

	clone.Base = "umutated"
	clone.Trace[0].PortId = "mutated"
	clone.Trace = clone.Trace[1:]

	require.Equal(t, "ugxusd", original.Base)
	require.Equal(t, "transwap", original.Trace[0].PortId)
	require.Len(t, original.Trace, 2)

	token := transwapv1.Token{Denom: original, Amount: "9"}
	tokenClone := CloneToken(token)
	tokenClone.Denom.Trace[1].ChannelId = "channel-99"

	require.Equal(t, "channel-1", token.Denom.Trace[1].ChannelId)
}

func TestFungibleTokenPacketDataBytes(t *testing.T) {
	packet := NewFungibleTokenPacketData("ugxusd", "42", "sender", "receiver", "memo")
	bz := FungibleTokenPacketDataBytes(packet)
	require.NotEmpty(t, bz)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(bz, &decoded))
	require.Equal(t, "ugxusd", decoded["denom"])
	require.Equal(t, "42", decoded["amount"])
	require.Equal(t, "sender", decoded["sender"])
	require.Equal(t, "receiver", decoded["receiver"])
	require.Equal(t, "memo", decoded["memo"])
}

func TestFungibleTokenPacketDataCustomPacketData(t *testing.T) {
	packet := NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", `{"fee":"3","depth":2}`)

	require.Equal(t, "3", FungibleTokenPacketDataCustomPacketData(packet, "fee"))
	require.Equal(t, float64(2), FungibleTokenPacketDataCustomPacketData(packet, "depth"))
	require.Nil(t, FungibleTokenPacketDataCustomPacketData(packet, "missing"))

	packet.Memo = "not-json"
	require.Nil(t, FungibleTokenPacketDataCustomPacketData(packet, "fee"))

	packet.Memo = ""
	require.Nil(t, FungibleTokenPacketDataCustomPacketData(packet, "fee"))
}

func TestInternalTransferRepresentationGettersAndValidationBranches(t *testing.T) {
	transfer := NewInternalTransferRepresentation("0", transwapv1.Token{Denom: NewDenom("ugxusd"), Amount: "1"}, "sender", "receiver", `{"k":"v"}`)

	require.Equal(t, "sender", transfer.GetPacketSender("sourcePort"))
	require.Equal(t, "v", transfer.GetCustomPacketData("k"))

	tests := []struct {
		name   string
		mutate func(*InternalTransferRepresentation)
	}{
		{"non numeric exchange id", func(data *InternalTransferRepresentation) { data.ExchangeID = "abc" }},
		{"blank sender", func(data *InternalTransferRepresentation) { data.Sender = " " }},
		{"blank receiver", func(data *InternalTransferRepresentation) { data.Receiver = "" }},
		{"receiver too long", func(data *InternalTransferRepresentation) {
			data.Receiver = strings.Repeat("r", MaximumReceiverLength+1)
		}},
		{"memo too long", func(data *InternalTransferRepresentation) {
			data.Memo = strings.Repeat("m", MaximumMemoLength+1)
		}},
		{"token amount zero", func(data *InternalTransferRepresentation) { data.Token.Amount = "0" }},
		{"token amount negative", func(data *InternalTransferRepresentation) { data.Token.Amount = "-1" }},
		{"nil token denom", func(data *InternalTransferRepresentation) { data.Token.Denom = nil }},
		{"invalid token denom", func(data *InternalTransferRepresentation) {
			data.Token.Denom = NewDenom("ugxusd", NewHop("bad port", "channel-0"))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := transfer
			data.Token = CloneToken(data.Token)
			tt.mutate(&data)
			require.Error(t, data.ValidateBasic())
		})
	}
}

func TestTokenToCoinSupportsValidToken(t *testing.T) {
	token := transwapv1.Token{Denom: NewDenom("ugxusd"), Amount: "123"}
	coin, err := TokenToCoin(token)
	require.NoError(t, err)
	require.Equal(t, "ugxusd", coin.Denom)
	require.Equal(t, sdkmath.NewInt(123), coin.Amount)
}

func TestMarshalPacketDataAndUnmarshalPacketDataRejectBadInputs(t *testing.T) {
	packet := NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")

	_, err := MarshalPacketData(packet, V1, "bad-encoding")
	require.Error(t, err)

	require.Panics(t, func() {
		_, _ = MarshalPacketData(packet, "bad-version", EncodingJSON)
	})

	_, err = UnmarshalPacketData([]byte("bad-json"), V1, EncodingJSON)
	require.Error(t, err)

	_, err = UnmarshalPacketData([]byte("bad"), "bad-version", EncodingJSON)
	require.Error(t, err)

	_, err = UnmarshalPacketData([]byte("bad"), V1, "bad-encoding")
	require.Error(t, err)
}

func TestValidateAmountBranches(t *testing.T) {
	require.NoError(t, validateAmount("1"))
	require.NoError(t, validateAmount("999999999999999999999999999999"))
	require.Error(t, validateAmount("invalid"))
	require.Error(t, validateAmount("0"))
	require.Error(t, validateAmount("-1"))
	require.Error(t, validateAmount(newOverflowAmount()))
}

func TestDenomPathRoundTripAndHash(t *testing.T) {
	tests := []*transwapv1.Denom{
		NewDenom("ugxusd"),
		NewDenom("ugxkrw", NewHop("transwap", "channel-0")),
		NewDenom("ugxusd", NewHop("transwap", "channel-0"), NewHop("transfer", "channel-17")),
	}

	for _, denom := range tests {
		t.Run(DenomPath(denom), func(t *testing.T) {
			path := DenomPath(denom)
			roundTrip := ExtractDenomFromPath(path)

			require.NoError(t, ValidateDenom(roundTrip))
			require.Equal(t, path, DenomPath(roundTrip))
			require.Equal(t, DenomHash(denom), DenomHash(roundTrip))
			require.Equal(t, DenomIBCDenom(denom), DenomIBCDenom(roundTrip))
		})
	}
}

func newOverflowAmount() string {
	overflow := UnboundedSpendLimit().BigInt()
	overflow.Add(overflow, big.NewInt(1))
	return overflow.String()
}
