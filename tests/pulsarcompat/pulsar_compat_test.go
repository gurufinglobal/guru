package pulsarcompat

import (
	"testing"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"google.golang.org/protobuf/proto"
)

var _ sdk.Msg = (*oraclev1.MsgUpsertTask)(nil)

func TestProtoCodecCanMarshalPulsarTypes(t *testing.T) {
	cdc := sdkcodec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	in := &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100}
	bz, err := cdc.Marshal(in)
	if err != nil {
		t.Fatalf("marshal pulsar message with ProtoCodec: %v", err)
	}
	if len(bz) == 0 {
		t.Fatalf("marshal produced empty bytes")
	}

	out := &oraclev1.Params{}
	if err := cdc.Unmarshal(bz, out); err != nil {
		t.Fatalf("unmarshal pulsar message with ProtoCodec: %v", err)
	}
	if !proto.Equal(out, in) {
		t.Fatalf("unexpected round-trip value: got=%+v want=%+v", out, in)
	}
}

func TestCollValueCanUsePulsarTypes(t *testing.T) {
	valueCodec := sdkcodec.CollValueV2[oraclev1.Params]()

	in := &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100}
	bz, err := valueCodec.Encode(in)
	if err != nil {
		t.Fatalf("collections encode failed: %v", err)
	}

	out, err := valueCodec.Decode(bz)
	if err != nil {
		t.Fatalf("collections decode failed: %v", err)
	}
	if !proto.Equal(out, in) {
		t.Fatalf("unexpected round-trip value: got=%+v want=%+v", out, in)
	}
}
