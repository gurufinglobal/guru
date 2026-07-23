package types

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"

	"github.com/stretchr/testify/require"
)

func TestValidateTokenRejectsInvalidDenomAndAmounts(t *testing.T) {
	valid := &Token{Denom: NewDenom("ugxusd"), Amount: "1"}

	tests := []struct {
		name   string
		mutate func(*Token)
	}{
		{"zero denom", func(token *Token) { token.Denom = Denom{} }},
		{"blank denom", func(token *Token) { token.Denom = NewDenom(" ") }},
		{"zero hop", func(token *Token) { token.Denom = NewDenom("ugxusd", Hop{}) }},
		{"invalid hop", func(token *Token) { token.Denom = NewDenom("ugxusd", NewHop("bad port", "channel-0")) }},
		{"zero amount", func(token *Token) { token.Amount = "0" }},
		{"negative amount", func(token *Token) { token.Amount = "-1" }},
		{"non numeric amount", func(token *Token) { token.Amount = "one" }},
		{"leading zero amount", func(token *Token) { token.Amount = "010" }},
		{"explicit plus amount", func(token *Token) { token.Amount = "+10" }},
		{"hex amount", func(token *Token) { token.Amount = "0x10" }},
		{"octal amount", func(token *Token) { token.Amount = "0o10" }},
		{"whitespace amount", func(token *Token) { token.Amount = " 10 " }},
		{"separator amount", func(token *Token) { token.Amount = "1_0" }},
		{"unicode amount", func(token *Token) { token.Amount = "１０" }},
		{"uint256 overflow amount", func(token *Token) { token.Amount = newOverflowAmount() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := CloneToken(valid)
			tt.mutate(token)

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
		mutate func(*FungibleTokenPacketData)
	}{
		{"blank denom", func(packet *FungibleTokenPacketData) { packet.Denom = " " }},
		{"invalid hop", func(packet *FungibleTokenPacketData) { packet.Denom = "bad port/channel-0/ugxusd" }},
		{"zero amount", func(packet *FungibleTokenPacketData) { packet.Amount = "0" }},
		{"negative amount", func(packet *FungibleTokenPacketData) { packet.Amount = "-1" }},
		{"non numeric amount", func(packet *FungibleTokenPacketData) { packet.Amount = "nan" }},
		{"leading zero amount", func(packet *FungibleTokenPacketData) { packet.Amount = "010" }},
		{"explicit plus amount", func(packet *FungibleTokenPacketData) { packet.Amount = "+10" }},
		{"hex amount", func(packet *FungibleTokenPacketData) { packet.Amount = "0x10" }},
		{"octal amount", func(packet *FungibleTokenPacketData) { packet.Amount = "0o10" }},
		{"whitespace amount", func(packet *FungibleTokenPacketData) { packet.Amount = " 10 " }},
		{"separator amount", func(packet *FungibleTokenPacketData) { packet.Amount = "1_0" }},
		{"unicode amount", func(packet *FungibleTokenPacketData) { packet.Amount = "１０" }},
		{"uint256 overflow amount", func(packet *FungibleTokenPacketData) { packet.Amount = newOverflowAmount() }},
		{"blank sender", func(packet *FungibleTokenPacketData) { packet.Sender = " " }},
		{"blank receiver", func(packet *FungibleTokenPacketData) { packet.Receiver = " " }},
		{"receiver too long", func(packet *FungibleTokenPacketData) {
			packet.Receiver = strings.Repeat("r", MaximumReceiverLength+1)
		}},
		{"memo too long", func(packet *FungibleTokenPacketData) {
			packet.Memo = strings.Repeat("m", MaximumMemoLength+1)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := cloneFungibleTokenPacketData(valid)
			tt.mutate(packet)

			require.Error(t, ValidateFungibleTokenPacketData(packet))
			_, err := PacketDataV1ToInternal(packet)
			require.Error(t, err)
		})
	}
}

func TestUnmarshalPacketDataRejectsNonCanonicalJSON(t *testing.T) {
	const valid = `{"exchange_id":"0","denom":"ugxusd","amount":"1","sender":"sender","receiver":"receiver","memo":""}`

	tests := []struct {
		name            string
		packet          string
		wantErrContains string
	}{
		{
			name:            "unknown field",
			packet:          `{"exchange_id":"0","denom":"ugxusd","amount":"1","sender":"sender","receiver":"receiver","memo":"","unexpected":"field"}`,
			wantErrContains: `unknown packet field "unexpected"`,
		},
		{
			name:            "duplicate known field",
			packet:          `{"exchange_id":"0","denom":"ugxusd","denom":"uatom","amount":"1","sender":"sender","receiver":"receiver","memo":""}`,
			wantErrContains: `duplicate packet field "denom"`,
		},
		{
			name:            "duplicate unknown field",
			packet:          `{"exchange_id":"0","denom":"ugxusd","amount":"1","sender":"sender","receiver":"receiver","memo":"","future":"one","future":"two"}`,
			wantErrContains: `duplicate packet field "future"`,
		},
		{
			name:            "trailing object",
			packet:          valid + `{}`,
			wantErrContains: "unexpected trailing JSON value",
		},
		{
			name:            "trailing scalar",
			packet:          valid + ` true`,
			wantErrContains: "unexpected trailing JSON value",
		},
		{
			name:            "non object",
			packet:          `[]`,
			wantErrContains: "expected JSON object",
		},
		{
			name:            "wrong field type",
			packet:          `{"exchange_id":"0","denom":7,"amount":"1","sender":"sender","receiver":"receiver","memo":""}`,
			wantErrContains: "cannot unmarshal number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalPacketData([]byte(tt.packet), V1, EncodingJSON)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErrContains)
		})
	}

	decoded, err := UnmarshalPacketData([]byte(" \n"+valid+"\t "), V1, EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, "0", decoded.ExchangeID)
	require.Equal(t, "ugxusd", decoded.Token.Denom.Base)
}

func TestUnmarshalFungibleTokenPacketDataJSONDoesNotPartiallyMutateTarget(t *testing.T) {
	target := NewFungibleTokenPacketData("uatom", "9", "original-sender", "original-receiver", "original-memo")
	want := cloneFungibleTokenPacketData(target)

	err := UnmarshalFungibleTokenPacketDataJSON(
		[]byte(`{"exchange_id":"0","denom":"ugxusd","denom":"uatom","amount":"1","sender":"sender","receiver":"receiver","memo":""}`),
		target,
	)
	require.Error(t, err)
	require.Equal(t, want, target)
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

func TestABIPacketDataOnlyEncodesCanonicalTransfers(t *testing.T) {
	packet := NewFungibleTokenPacketData("transwap/channel-0/ugxusd", "123", "sender", "receiver", "memo")

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

	for _, exchangeID := range []string{"7", "", "00", "01", "+1"} {
		packet.ExchangeId = exchangeID
		_, err = MarshalPacketData(packet, V1, EncodingABI)
		require.Error(t, err, exchangeID)
	}

	packet.ExchangeId = "0"
	for _, amount := range []string{"0", "00", "010", "+10", "-1", " 10", "10 ", "0x10", "0o10", "1_0", "１０"} {
		packet.Amount = amount
		_, err = MarshalPacketData(packet, V1, EncodingABI)
		require.Error(t, err, amount)
	}

	packet.Amount = "123"
	_, err = UnmarshalPacketData([]byte{0x01, 0x02, 0x03}, V1, EncodingABI)
	require.Error(t, err)

	packet.Amount = "not-a-number"
	_, err = MarshalPacketData(packet, V1, EncodingABI)
	require.Error(t, err)

	_, err = EncodeABIFungibleTokenPacketData(nil)
	require.Error(t, err)
}

func TestUnmarshalPacketDataRejectsProtobufUnknownFields(t *testing.T) {
	packet := NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")
	bz, err := packet.Marshal()
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

	nonNumeric := NewInternalTransferRepresentation("abc", &Token{Denom: NewDenom("ugxusd"), Amount: "1"}, "sender", "receiver", "")
	require.Error(t, nonNumeric.ValidateBasic())
}

func TestClassifyExchangeIDRequiresCanonicalDiscriminator(t *testing.T) {
	tests := []struct {
		raw        string
		kind       PacketKind
		exchangeID uint64
		valid      bool
	}{
		{raw: "0", kind: PacketKindTransfer, valid: true},
		{raw: "1", kind: PacketKindExchange, exchangeID: 1, valid: true},
		{raw: "18446744073709551615", kind: PacketKindExchange, exchangeID: ^uint64(0), valid: true},
		{raw: ""},
		{raw: " "},
		{raw: "+1"},
		{raw: "-1"},
		{raw: "00"},
		{raw: "01"},
		{raw: "abc"},
		{raw: "18446744073709551616"},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			kind, exchangeID, err := ClassifyExchangeID(tt.raw)
			if !tt.valid {
				require.ErrorIs(t, err, ErrInvalidExchangeID)
				require.Equal(t, PacketKindUnspecified, kind)
				require.Zero(t, exchangeID)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.kind, kind)
			require.Equal(t, tt.exchangeID, exchangeID)
		})
	}
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

	token := Token{Denom: original, Amount: "9"}
	tokenClone := CloneToken(&token)
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
	transfer := NewInternalTransferRepresentation("0", &Token{Denom: NewDenom("ugxusd"), Amount: "1"}, "sender", "receiver", `{"k":"v"}`)

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
		{"zero token denom", func(data *InternalTransferRepresentation) { data.Token.Denom = Denom{} }},
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
	token := Token{Denom: NewDenom("ugxusd"), Amount: "123"}
	coin, err := TokenToCoin(&token)
	require.NoError(t, err)
	require.Equal(t, "ugxusd", coin.Denom)
	require.Equal(t, sdkmath.NewInt(123), coin.Amount)
}

func TestTokenToCoinValidatesOnlyResolvedLocalBankDenom(t *testing.T) {
	_, err := TokenToCoin(&Token{Denom: NewDenom("!"), Amount: "1"})
	require.ErrorIs(t, err, ErrInvalidDenomForTransfer)
	require.ErrorContains(t, err, "cannot be materialized as a local bank coin")

	remoteDenom := NewDenom("!", NewHop(PortID, "channel-0"))
	coin, err := TokenToCoin(&Token{Denom: remoteDenom, Amount: "1"})
	require.NoError(t, err)
	require.Equal(t, DenomIBCDenom(remoteDenom), coin.Denom)
	require.Equal(t, sdkmath.OneInt(), coin.Amount)
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
	for _, invalid := range []string{
		"",
		"invalid",
		"0",
		"-1",
		"+1",
		"00",
		"01",
		"010",
		" 10",
		"10 ",
		"0x10",
		"0o10",
		"1_0",
		"1e1",
		"１０",
		newOverflowAmount(),
	} {
		require.Error(t, validateAmount(invalid), invalid)
	}
}

func TestDenomPathRoundTripAndHash(t *testing.T) {
	tests := []Denom{
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

func FuzzUnmarshalFungibleTokenPacketDataJSONNeverPanics(f *testing.F) {
	for _, seed := range [][]byte{
		FungibleTokenPacketDataBytes(NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")),
		[]byte(`{"exchange_id":"0","denom":"a","denom":"b","amount":"1","sender":"s","receiver":"r"}`),
		[]byte(`{"exchange_id":"0","denom":"ugxusd","amount":"1","sender":"s","receiver":"r"}{}`),
		[]byte(`not-json`),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		var packet FungibleTokenPacketData
		if err := UnmarshalFungibleTokenPacketDataJSON(raw, &packet); err != nil {
			return
		}

		canonical, err := json.Marshal(&packet)
		if err != nil {
			t.Fatalf("marshal accepted packet: %v", err)
		}
		var roundTrip FungibleTokenPacketData
		if err := UnmarshalFungibleTokenPacketDataJSON(canonical, &roundTrip); err != nil {
			t.Fatalf("decoder rejected its canonical output: %v", err)
		}
	})
}

func FuzzTokenToCoinNeverPanics(f *testing.F) {
	f.Add("uatom", false)
	f.Add("!", false)
	f.Add("!", true)
	f.Add("", true)

	f.Fuzz(func(t *testing.T, base string, traced bool) {
		denom := NewDenom(base)
		if traced {
			denom.Trace = []Hop{NewHop(PortID, "channel-0")}
		}
		_, _ = TokenToCoin(&Token{Denom: denom, Amount: "1"})
	})
}

func cloneFungibleTokenPacketData(packet *FungibleTokenPacketData) *FungibleTokenPacketData {
	if packet == nil {
		return nil
	}
	bz, err := packet.Marshal()
	if err != nil {
		panic(err)
	}
	cloned := &FungibleTokenPacketData{}
	if err := cloned.Unmarshal(bz); err != nil {
		panic(err)
	}
	return cloned
}
