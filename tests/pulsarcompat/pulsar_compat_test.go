package pulsarcompat

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	pulsarbex "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	pulsarconstitution "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	pulsarfeepolicy "github.com/gurufinglobal/guru/v3/api/guru/feepolicy/v1"
	pulsaroracle "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	pulsartranswap "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type gogoWireMessage interface {
	Reset()
	String() string
	ProtoMessage()
	Marshal() ([]byte, error)
	Size() int
	XXX_Marshal([]byte, bool) ([]byte, error)
	Unmarshal([]byte) error
}

func TestInternalGogoAndPublicPulsarWireParity(t *testing.T) {
	minBond := sdk.NewInt64Coin("agxn", 10)
	discountAmount := sdkmath.LegacyMustNewDecFromStr("25.125")

	tests := []struct {
		name      string
		fullName  string
		gogo      gogoWireMessage
		newGogo   func() gogoWireMessage
		pulsar    protov2.Message
		newPulsar func() protov2.Message
	}{
		{
			name: "bex exchange state", fullName: "guru.bex.v1.Exchange",
			gogo:      &bextypes.Exchange{Id: 7, AdminAddress: "guru1admin", Metadata: map[string]string{"network": "test", "tier": "one"}},
			newGogo:   func() gogoWireMessage { return &bextypes.Exchange{} },
			pulsar:    &pulsarbex.Exchange{Id: 7, AdminAddress: "guru1admin", Metadata: map[string]string{"network": "test", "tier": "one"}},
			newPulsar: func() protov2.Message { return &pulsarbex.Exchange{} },
		},
		{
			name: "bex fee ledger", fullName: "guru.bex.v1.FeeLedger",
			gogo:      &bextypes.FeeLedger{Coins: sdk.NewCoins(sdk.NewInt64Coin("agxn", 10))},
			newGogo:   func() gogoWireMessage { return &bextypes.FeeLedger{} },
			pulsar:    &pulsarbex.FeeLedger{Coins: []*basev1beta1.Coin{{Denom: "agxn", Amount: "10"}}},
			newPulsar: func() protov2.Message { return &pulsarbex.FeeLedger{} },
		},
		{
			name: "bex nested update msg", fullName: "guru.bex.v1.MsgUpdateExchange",
			gogo: &bextypes.MsgUpdateExchange{
				AdminAddress: "guru1admin", ExchangeId: 7, ExpectedRevision: 3,
				Patch: &bextypes.ExchangeUpdatePatch{DenomA: bextypes.NewStringValue("uatom"), Metadata: map[string]string{"network": "test", "tier": "one"}},
			},
			newGogo: func() gogoWireMessage { return &bextypes.MsgUpdateExchange{} },
			pulsar: &pulsarbex.MsgUpdateExchange{
				AdminAddress: "guru1admin", ExchangeId: 7, ExpectedRevision: 3,
				Patch: &pulsarbex.ExchangeUpdatePatch{DenomA: wrapperspb.String("uatom"), Metadata: map[string]string{"network": "test", "tier": "one"}},
			},
			newPulsar: func() protov2.Message { return &pulsarbex.MsgUpdateExchange{} },
		},
		{
			name: "constitution params", fullName: "guru.constitution.v1.Params",
			gogo:      &constitutiontypes.Params{MinValidatorBondAmount: &minBond},
			newGogo:   func() gogoWireMessage { return &constitutiontypes.Params{} },
			pulsar:    &pulsarconstitution.Params{MinValidatorBondAmount: &basev1beta1.Coin{Denom: "agxn", Amount: "10"}},
			newPulsar: func() protov2.Message { return &pulsarconstitution.Params{} },
		},
		{
			name: "constitution ratio", fullName: "guru.constitution.v1.SeparationRatio",
			gogo:      &constitutiontypes.SeparationRatio{BasePpm: 100_000, BurnPpm: 200_000, ValidatorsPpm: 700_000},
			newGogo:   func() gogoWireMessage { return &constitutiontypes.SeparationRatio{} },
			pulsar:    &pulsarconstitution.SeparationRatio{BasePpm: 100_000, BurnPpm: 200_000, ValidatorsPpm: 700_000},
			newPulsar: func() protov2.Message { return &pulsarconstitution.SeparationRatio{} },
		},
		{
			name: "constitution schedule", fullName: "guru.constitution.v1.MinGasPriceSchedule",
			gogo:      &constitutiontypes.MinGasPriceSchedule{EffectiveHeight: 25, ScheduledMinGasPrice: "630000000000", SourceSymbol: "GURU/USD", SourceValue: "1.0", SourceOracleHeight: 20, SourceSubmissionIntervalBlocks: 5, PendingDelayBlocks: 5, PendingDelayCapBlocks: 10, RawMinGasPrice: "630000000000", PreviousMinGasPrice: "630000000000"},
			newGogo:   func() gogoWireMessage { return &constitutiontypes.MinGasPriceSchedule{} },
			pulsar:    &pulsarconstitution.MinGasPriceSchedule{EffectiveHeight: 25, ScheduledMinGasPrice: "630000000000", SourceSymbol: "GURU/USD", SourceValue: "1.0", SourceOracleHeight: 20, SourceSubmissionIntervalBlocks: 5, PendingDelayBlocks: 5, PendingDelayCapBlocks: 10, RawMinGasPrice: "630000000000", PreviousMinGasPrice: "630000000000"},
			newPulsar: func() protov2.Message { return &pulsarconstitution.MinGasPriceSchedule{} },
		},
		{
			name: "feepolicy discount", fullName: "guru.feepolicy.v1.Discount",
			gogo: &feepolicytypes.Discount{
				DiscountType: "percent",
				MsgType:      "/cosmos.bank.v1beta1.MsgSend",
				Amount:       discountAmount,
			},
			newGogo: func() gogoWireMessage { return &feepolicytypes.Discount{} },
			// LegacyDec's gogo custom type encodes its 10^18-scaled integer as
			// the protobuf string. Pulsar exposes that exact wire value.
			pulsar: &pulsarfeepolicy.Discount{
				DiscountType: "percent",
				MsgType:      "/cosmos.bank.v1beta1.MsgSend",
				Amount:       discountAmount.BigInt().String(),
			},
			newPulsar: func() protov2.Message { return &pulsarfeepolicy.Discount{} },
		},
		{
			name: "feepolicy nested register msg", fullName: "guru.feepolicy.v1.MsgRegisterDiscounts",
			gogo: &feepolicytypes.MsgRegisterDiscounts{
				ModeratorAddress: "guru1moderator",
				Discounts: []feepolicytypes.AccountDiscount{{
					Address: "guru1account",
					Modules: []feepolicytypes.ModuleDiscount{{
						Module: "bank",
						Discounts: []feepolicytypes.Discount{{
							DiscountType: "percent",
							MsgType:      "/cosmos.bank.v1beta1.MsgSend",
							Amount:       discountAmount,
						}},
					}},
				}},
			},
			newGogo: func() gogoWireMessage { return &feepolicytypes.MsgRegisterDiscounts{} },
			pulsar: &pulsarfeepolicy.MsgRegisterDiscounts{
				ModeratorAddress: "guru1moderator",
				Discounts: []*pulsarfeepolicy.AccountDiscount{{
					Address: "guru1account",
					Modules: []*pulsarfeepolicy.ModuleDiscount{{
						Module: "bank",
						Discounts: []*pulsarfeepolicy.Discount{{
							DiscountType: "percent",
							MsgType:      "/cosmos.bank.v1beta1.MsgSend",
							Amount:       discountAmount.BigInt().String(),
						}},
					}},
				}},
			},
			newPulsar: func() protov2.Message { return &pulsarfeepolicy.MsgRegisterDiscounts{} },
		},
		{
			name: "oracle task", fullName: "guru.oracle.v1.OracleTask",
			gogo:      &oracletypes.OracleTask{Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 5},
			newGogo:   func() gogoWireMessage { return &oracletypes.OracleTask{} },
			pulsar:    &pulsaroracle.OracleTask{Symbol: "BTC/USD", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 5},
			newPulsar: func() protov2.Message { return &pulsaroracle.OracleTask{} },
		},
		{
			name: "oracle value", fullName: "guru.oracle.v1.OracleValue",
			gogo:      &oracletypes.OracleValue{Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", BlockHeight: 12, BlockTimeUnix: 34},
			newGogo:   func() gogoWireMessage { return &oracletypes.OracleValue{} },
			pulsar:    &pulsaroracle.OracleValue{Symbol: "BTC/USD", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", BlockHeight: 12, BlockTimeUnix: 34},
			newPulsar: func() protov2.Message { return &pulsaroracle.OracleValue{} },
		},
		{
			name: "oracle history", fullName: "guru.oracle.v1.OracleHistory",
			gogo:      &oracletypes.OracleHistory{Symbol: "BTC/USD", Values: []*oracletypes.OracleValue{{Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", BlockHeight: 12}}},
			newGogo:   func() gogoWireMessage { return &oracletypes.OracleHistory{} },
			pulsar:    &pulsaroracle.OracleHistory{Symbol: "BTC/USD", Values: []*pulsaroracle.OracleValue{{Symbol: "BTC/USD", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", BlockHeight: 12}}},
			newPulsar: func() protov2.Message { return &pulsaroracle.OracleHistory{} },
		},
		{
			name: "vote extension", fullName: "guru.oracle.v1.OracleVoteExtension",
			gogo:      &oracletypes.OracleVoteExtension{Results: []*oracletypes.OracleValidatorResult{{Symbol: "BTC/USD", Value: "65000.25", SourceCount: 3}}},
			newGogo:   func() gogoWireMessage { return &oracletypes.OracleVoteExtension{} },
			pulsar:    &pulsaroracle.OracleVoteExtension{Results: []*pulsaroracle.OracleValidatorResult{{Symbol: "BTC/USD", Value: "65000.25", SourceCount: 3}}},
			newPulsar: func() protov2.Message { return &pulsaroracle.OracleVoteExtension{} },
		},
		{
			name: "proposal payload", fullName: "guru.oracle.v1.OracleProposalPayload",
			gogo:      &oracletypes.OracleProposalPayload{Height: 12, VoteExtensions: &oracletypes.OracleSignedVoteExtensions{Round: 1, Votes: []*oracletypes.OracleSignedVoteExtension{{ValidatorAddress: []byte{1, 2}, ValidatorPower: 10, BlockIdFlag: 2, VoteExtension: []byte{3}, ExtensionSignature: []byte{4}}}}, Values: []*oracletypes.OracleValue{{Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", BlockHeight: 12}}},
			newGogo:   func() gogoWireMessage { return &oracletypes.OracleProposalPayload{} },
			pulsar:    &pulsaroracle.OracleProposalPayload{Height: 12, VoteExtensions: &pulsaroracle.OracleSignedVoteExtensions{Round: 1, Votes: []*pulsaroracle.OracleSignedVoteExtension{{ValidatorAddress: []byte{1, 2}, ValidatorPower: 10, BlockIdFlag: 2, VoteExtension: []byte{3}, ExtensionSignature: []byte{4}}}}, Values: []*pulsaroracle.OracleValue{{Symbol: "BTC/USD", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", BlockHeight: 12}}},
			newPulsar: func() protov2.Message { return &pulsaroracle.OracleProposalPayload{} },
		},
		{
			name: "oracle nested msg", fullName: "guru.oracle.v1.MsgUpsertTask",
			gogo:      &oracletypes.MsgUpsertTask{Moderator: "guru1moderator", Task: &oracletypes.OracleTask{Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 5}},
			newGogo:   func() gogoWireMessage { return &oracletypes.MsgUpsertTask{} },
			pulsar:    &pulsaroracle.MsgUpsertTask{Moderator: "guru1moderator", Task: &pulsaroracle.OracleTask{Symbol: "BTC/USD", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 5}},
			newPulsar: func() protov2.Message { return &pulsaroracle.MsgUpsertTask{} },
		},
		{
			name: "constitution nested msg", fullName: "guru.constitution.v1.MsgUpdateParams",
			gogo:      &constitutiontypes.MsgUpdateParams{Authority: "guru1authority", Params: &constitutiontypes.Params{MinValidatorBondAmount: &minBond}},
			newGogo:   func() gogoWireMessage { return &constitutiontypes.MsgUpdateParams{} },
			pulsar:    &pulsarconstitution.MsgUpdateParams{Authority: "guru1authority", Params: &pulsarconstitution.Params{MinValidatorBondAmount: &basev1beta1.Coin{Denom: "agxn", Amount: "10"}}},
			newPulsar: func() protov2.Message { return &pulsarconstitution.MsgUpdateParams{} },
		},
		{
			name: "sidecar request", fullName: "guru.oracle.v1.GetAggregatesRequest",
			gogo:      &oracletypes.GetAggregatesRequest{Symbols: []string{"BTC/USD", "ETH/USD"}},
			newGogo:   func() gogoWireMessage { return &oracletypes.GetAggregatesRequest{} },
			pulsar:    &pulsaroracle.GetAggregatesRequest{Symbols: []string{"BTC/USD", "ETH/USD"}},
			newPulsar: func() protov2.Message { return &pulsaroracle.GetAggregatesRequest{} },
		},
		{
			name: "sidecar response", fullName: "guru.oracle.v1.GetAggregatesResponse",
			gogo:      &oracletypes.GetAggregatesResponse{Results: []*oracletypes.AggregatedResult{{Symbol: "BTC/USD", Value: "65000.25", SourceCount: 3}}},
			newGogo:   func() gogoWireMessage { return &oracletypes.GetAggregatesResponse{} },
			pulsar:    &pulsaroracle.GetAggregatesResponse{Results: []*pulsaroracle.AggregatedResult{{Symbol: "BTC/USD", Value: "65000.25", SourceCount: 3}}},
			newPulsar: func() protov2.Message { return &pulsaroracle.GetAggregatesResponse{} },
		},
		{
			name: "transwap denom", fullName: "guru.transwap.v1.Denom",
			gogo:      &transwaptypes.Denom{Base: "agxn", Trace: []transwaptypes.Hop{{PortId: "transwap", ChannelId: "channel-0"}}},
			newGogo:   func() gogoWireMessage { return &transwaptypes.Denom{} },
			pulsar:    &pulsartranswap.Denom{Base: "agxn", Trace: []*pulsartranswap.Hop{{PortId: "transwap", ChannelId: "channel-0"}}},
			newPulsar: func() protov2.Message { return &pulsartranswap.Denom{} },
		},
		{
			name: "transwap refund record", fullName: "guru.transwap.v1.RefundRecord",
			gogo: &transwaptypes.RefundRecord{
				Id: "refund-1", Status: transwaptypes.RefundStatus_REFUND_STATUS_PENDING,
				Token:             transwaptypes.Token{Denom: transwaptypes.Denom{Base: "agxn"}, Amount: "10"},
				OriginalFee:       sdk.NewInt64Coin("agxn", 1),
				VolumeReservation: &bextypes.VolumeReservation{ExchangeId: 7, Direction: bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B, EpochStartUnix: 100, EpochSeconds: 10, Amount: "9", VolumeWindowGeneration: 2},
			},
			newGogo: func() gogoWireMessage { return &transwaptypes.RefundRecord{} },
			pulsar: &pulsartranswap.RefundRecord{
				Id: "refund-1", Status: pulsartranswap.RefundStatus_REFUND_STATUS_PENDING,
				Token:             &pulsartranswap.Token{Denom: &pulsartranswap.Denom{Base: "agxn"}, Amount: "10"},
				OriginalFee:       &basev1beta1.Coin{Denom: "agxn", Amount: "1"},
				VolumeReservation: &pulsarbex.VolumeReservation{ExchangeId: 7, Direction: pulsarbex.SwapDirection_SWAP_DIRECTION_A_TO_B, EpochStartUnix: 100, EpochSeconds: 10, Amount: "9", VolumeWindowGeneration: 2},
			},
			newPulsar: func() protov2.Message { return &pulsartranswap.RefundRecord{} },
		},
		{
			name: "transwap nested params msg", fullName: "guru.transwap.v1.MsgUpdateParams",
			gogo:      &transwaptypes.MsgUpdateParams{Authority: "guru1authority", Params: &transwaptypes.Params{MaxRefundRetries: 3, RefundTimeoutWindow: 60, MinRelaySafetyMargin: 5}},
			newGogo:   func() gogoWireMessage { return &transwaptypes.MsgUpdateParams{} },
			pulsar:    &pulsartranswap.MsgUpdateParams{Authority: "guru1authority", Params: &pulsartranswap.Params{MaxRefundRetries: 3, RefundTimeoutWindow: 60, MinRelaySafetyMargin: 5}},
			newPulsar: func() protov2.Message { return &pulsartranswap.MsgUpdateParams{} },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertWireParity(t, tc.fullName, tc.gogo, tc.newGogo, tc.pulsar, tc.newPulsar)
		})
	}
}

func TestInternalGogoAndPublicPulsarDescriptorsMatch(t *testing.T) {
	files := []string{
		"guru/bex/v1/bex.proto",
		"guru/bex/v1/genesis.proto",
		"guru/bex/v1/query.proto",
		"guru/bex/v1/tx.proto",
		"guru/constitution/v1/genesis.proto",
		"guru/constitution/v1/params.proto",
		"guru/constitution/v1/query.proto",
		"guru/constitution/v1/tx.proto",
		"guru/constitution/v1/types.proto",
		"guru/feepolicy/v1/feepolicy.proto",
		"guru/feepolicy/v1/genesis.proto",
		"guru/feepolicy/v1/query.proto",
		"guru/feepolicy/v1/tx.proto",
		"guru/oracle/v1/daemon.proto",
		"guru/oracle/v1/genesis.proto",
		"guru/oracle/v1/oracle.proto",
		"guru/oracle/v1/params.proto",
		"guru/oracle/v1/query.proto",
		"guru/oracle/v1/tx.proto",
		"guru/oracle/v1/vote_extension.proto",
		"guru/transwap/v1/denomtrace.proto",
		"guru/transwap/v1/genesis.proto",
		"guru/transwap/v1/packet.proto",
		"guru/transwap/v1/params.proto",
		"guru/transwap/v1/query.proto",
		"guru/transwap/v1/refund.proto",
		"guru/transwap/v1/token.proto",
		"guru/transwap/v1/tx.proto",
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			compressed := gogoproto.FileDescriptor(file)
			if len(compressed) == 0 {
				t.Fatalf("internal gogo descriptor is not registered")
			}
			reader, err := gzip.NewReader(bytes.NewReader(compressed))
			if err != nil {
				t.Fatalf("open internal descriptor: %v", err)
			}
			internalBytes, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read internal descriptor: %v", err)
			}
			if err := reader.Close(); err != nil {
				t.Fatalf("close internal descriptor: %v", err)
			}
			var internal descriptorpb.FileDescriptorProto
			if err := protov2.Unmarshal(internalBytes, &internal); err != nil {
				t.Fatalf("decode internal descriptor: %v", err)
			}

			publicFile, err := protoregistry.GlobalFiles.FindFileByPath(file)
			if err != nil {
				t.Fatalf("public Pulsar descriptor is not registered: %v", err)
			}
			public := protodesc.ToFileDescriptorProto(publicFile)

			// Code generators attach language/runtime file options; messages, enums, services, and their options must match.
			internal.Options = nil
			public.Options = nil
			internal.SourceCodeInfo = nil
			public.SourceCodeInfo = nil
			if !protov2.Equal(&internal, public) {
				t.Fatalf("internal gogo and public Pulsar descriptors differ:\ninternal=%v\npublic=%v", &internal, public)
			}
		})
	}
}

func assertWireParity(
	t *testing.T,
	fullName string,
	gogo gogoWireMessage,
	newGogo func() gogoWireMessage,
	pulsar protov2.Message,
	newPulsar func() protov2.Message,
) {
	t.Helper()

	gogoBz, err := gogo.XXX_Marshal(make([]byte, 0, gogo.Size()), true)
	if err != nil {
		t.Fatalf("marshal internal gogo message: %v", err)
	}
	pulsarBz, err := (protov2.MarshalOptions{Deterministic: true}).Marshal(pulsar)
	if err != nil {
		t.Fatalf("marshal public Pulsar message: %v", err)
	}
	if !bytes.Equal(gogoBz, pulsarBz) {
		t.Fatalf("deterministic wire bytes differ: gogo=%x pulsar=%x", gogoBz, pulsarBz)
	}
	gogoFromPulsar := newGogo()
	if err := gogoFromPulsar.Unmarshal(pulsarBz); err != nil {
		t.Fatalf("unmarshal public bytes into internal gogo message: %v", err)
	}
	gogoFromPulsarBz, err := gogoFromPulsar.XXX_Marshal(make([]byte, 0, gogoFromPulsar.Size()), true)
	if err != nil {
		t.Fatalf("re-marshal public bytes from internal gogo message: %v", err)
	}
	publicRoundTrip := newPulsar()
	if err := protov2.Unmarshal(gogoFromPulsarBz, publicRoundTrip); err != nil {
		t.Fatalf("unmarshal internal cross-round-trip into public message: %v", err)
	}
	if !protov2.Equal(pulsar, publicRoundTrip) {
		t.Fatalf("public-to-internal cross-round-trip differs: got=%v want=%v", publicRoundTrip, pulsar)
	}

	pulsarFromGogo := newPulsar()
	if err := protov2.Unmarshal(gogoBz, pulsarFromGogo); err != nil {
		t.Fatalf("unmarshal internal bytes into public Pulsar message: %v", err)
	}
	if !protov2.Equal(pulsar, pulsarFromGogo) {
		t.Fatalf("public Pulsar cross-round-trip differs: got=%v want=%v", pulsarFromGogo, pulsar)
	}

	packed, err := codectypes.NewAnyWithValue(gogo)
	if err != nil {
		t.Fatalf("pack internal gogo message: %v", err)
	}
	if packed.TypeUrl != "/"+fullName {
		t.Fatalf("internal type URL differs: got=%q want=%q", packed.TypeUrl, "/"+fullName)
	}
	if got := string(pulsar.ProtoReflect().Descriptor().FullName()); got != fullName {
		t.Fatalf("public full name differs: got=%q want=%q", got, fullName)
	}
}
