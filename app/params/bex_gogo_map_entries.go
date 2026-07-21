package params

import (
	"sync"

	gogoproto "github.com/cosmos/gogoproto/proto"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
)

var registerBexGogoMapEntriesOnce sync.Once

// registerBexGogoMapEntries makes BEX's generated map-entry descriptors usable by
// the SDK's strict transaction decoder. gogoproto registers synthetic map
// entries as Go map types, while unknownproto requires each nested entry to be
// a proto.Message. Registering descriptor-only prototypes keeps strict nested
// unknown-field validation enabled without replacing the standard TxConfig.
func registerBexGogoMapEntries() {
	registerBexGogoMapEntriesOnce.Do(func() {
		gogoproto.RegisterType((*bexExchangeMetadataEntry)(nil), "guru.bex.v1.Exchange.MetadataEntry")
		gogoproto.RegisterType((*bexExchangeUpdatePatchMetadataEntry)(nil), "guru.bex.v1.ExchangeUpdatePatch.MetadataEntry")
		gogoproto.RegisterType((*bexMsgRegisterExchangeMetadataEntry)(nil), "guru.bex.v1.MsgRegisterExchange.MetadataEntry")
	})
}

type bexExchangeMetadataEntry struct {
	Key   string `protobuf:"bytes,1,opt,name=key,proto3"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3"`
}

func (m *bexExchangeMetadataEntry) Reset()         { *m = bexExchangeMetadataEntry{} }
func (m *bexExchangeMetadataEntry) String() string { return gogoproto.CompactTextString(m) }
func (*bexExchangeMetadataEntry) ProtoMessage()    {}
func (*bexExchangeMetadataEntry) Descriptor() ([]byte, []int) {
	descriptor, path := (&bextypes.Exchange{}).Descriptor()
	return descriptor, append(path, 0)
}

type bexExchangeUpdatePatchMetadataEntry struct {
	Key   string `protobuf:"bytes,1,opt,name=key,proto3"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3"`
}

func (m *bexExchangeUpdatePatchMetadataEntry) Reset() {
	*m = bexExchangeUpdatePatchMetadataEntry{}
}
func (m *bexExchangeUpdatePatchMetadataEntry) String() string {
	return gogoproto.CompactTextString(m)
}
func (*bexExchangeUpdatePatchMetadataEntry) ProtoMessage() {}
func (*bexExchangeUpdatePatchMetadataEntry) Descriptor() ([]byte, []int) {
	descriptor, path := (&bextypes.ExchangeUpdatePatch{}).Descriptor()
	return descriptor, append(path, 0)
}

type bexMsgRegisterExchangeMetadataEntry struct {
	Key   string `protobuf:"bytes,1,opt,name=key,proto3"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3"`
}

func (m *bexMsgRegisterExchangeMetadataEntry) Reset() {
	*m = bexMsgRegisterExchangeMetadataEntry{}
}
func (m *bexMsgRegisterExchangeMetadataEntry) String() string {
	return gogoproto.CompactTextString(m)
}
func (*bexMsgRegisterExchangeMetadataEntry) ProtoMessage() {}
func (*bexMsgRegisterExchangeMetadataEntry) Descriptor() ([]byte, []int) {
	descriptor, path := (&bextypes.MsgRegisterExchange{}).Descriptor()
	return descriptor, append(path, 0)
}
