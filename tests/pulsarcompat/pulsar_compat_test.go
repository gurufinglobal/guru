package pulsarcompat

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	pulsarconstitution "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	pulsaroracle "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

type gogoWireMessage interface {
	Reset()
	String() string
	ProtoMessage()
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

func TestInternalGogoAndPublicPulsarWireParity(t *testing.T) {
	minBond := sdk.NewInt64Coin("agxn", 10)

	tests := []struct {
		name      string
		fullName  string
		gogo      gogoWireMessage
		newGogo   func() gogoWireMessage
		pulsar    protov2.Message
		newPulsar func() protov2.Message
	}{
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
			gogo:      &oracletypes.OracleVoteExtension{Results: []*oracletypes.OracleValidatorResult{{Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", SourceCount: 3}}},
			newGogo:   func() gogoWireMessage { return &oracletypes.OracleVoteExtension{} },
			pulsar:    &pulsaroracle.OracleVoteExtension{Results: []*pulsaroracle.OracleValidatorResult{{Symbol: "BTC/USD", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", SourceCount: 3}}},
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
			name: "sidecar request", fullName: "guru.oracle.v1.GetSamplesRequest",
			gogo:      &oracletypes.GetSamplesRequest{Tasks: []*oracletypes.OracleTask{{Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 5}}, Height: 12},
			newGogo:   func() gogoWireMessage { return &oracletypes.GetSamplesRequest{} },
			pulsar:    &pulsaroracle.GetSamplesRequest{Tasks: []*pulsaroracle.OracleTask{{Symbol: "BTC/USD", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 5}}, Height: 12},
			newPulsar: func() protov2.Message { return &pulsaroracle.GetSamplesRequest{} },
		},
		{
			name: "sidecar response", fullName: "guru.oracle.v1.GetSamplesResponse",
			gogo:      &oracletypes.GetSamplesResponse{Symbols: []*oracletypes.OracleSymbolSamples{{Symbol: "BTC/USD", Samples: []*oracletypes.OracleSample{{Source: "source-a", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", SampleTimeUnix: 34}}}}},
			newGogo:   func() gogoWireMessage { return &oracletypes.GetSamplesResponse{} },
			pulsar:    &pulsaroracle.GetSamplesResponse{Symbols: []*pulsaroracle.OracleSymbolSamples{{Symbol: "BTC/USD", Samples: []*pulsaroracle.OracleSample{{Source: "source-a", ValueType: pulsaroracle.ValueType_VALUE_TYPE_NUMERIC, Value: "65000.25", SampleTimeUnix: 34}}}}},
			newPulsar: func() protov2.Message { return &pulsaroracle.GetSamplesResponse{} },
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
		"guru/constitution/v1/genesis.proto",
		"guru/constitution/v1/params.proto",
		"guru/constitution/v1/query.proto",
		"guru/constitution/v1/tx.proto",
		"guru/constitution/v1/types.proto",
		"guru/oracle/v1/daemon.proto",
		"guru/oracle/v1/genesis.proto",
		"guru/oracle/v1/oracle.proto",
		"guru/oracle/v1/params.proto",
		"guru/oracle/v1/query.proto",
		"guru/oracle/v1/tx.proto",
		"guru/oracle/v1/vote_extension.proto",
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

	gogoBz, err := gogo.Marshal()
	if err != nil {
		t.Fatalf("marshal internal gogo message: %v", err)
	}
	pulsarBz, err := (protov2.MarshalOptions{Deterministic: true}).Marshal(pulsar)
	if err != nil {
		t.Fatalf("marshal public Pulsar message: %v", err)
	}
	if string(gogoBz) != string(pulsarBz) {
		t.Fatalf("wire bytes differ: gogo=%x pulsar=%x", gogoBz, pulsarBz)
	}

	gogoFromPulsar := newGogo()
	if err := gogoFromPulsar.Unmarshal(pulsarBz); err != nil {
		t.Fatalf("unmarshal public bytes into internal gogo message: %v", err)
	}
	gogoRoundTrip, err := gogoFromPulsar.Marshal()
	if err != nil || string(gogoRoundTrip) != string(gogoBz) {
		t.Fatalf("internal gogo cross-round-trip failed: err=%v bytes=%x", err, gogoRoundTrip)
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
