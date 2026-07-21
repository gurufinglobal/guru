package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
	legacyproto "github.com/golang/protobuf/proto" //nolint:staticcheck // grpc-gateway v1 requires its legacy enum registry.
)

const refundStatusProtoName = "guru.transwap.v1.RefundStatus"

//nolint:staticcheck // grpc-gateway v1 resolves query-string enums only through the legacy registry.
func init() {
	// grpc-gateway v1 resolves query-string enum names through the legacy
	// github.com/golang/protobuf registry, while protoc-gen-gogo registers only
	// with the Cosmos gogo registry. Bridge the generated enum without changing
	// generated files so REST queries retain symbolic enum support.
	if legacyproto.EnumValueMap(refundStatusProtoName) == nil {
		legacyproto.RegisterEnum(refundStatusProtoName, RefundStatus_name, RefundStatus_value)
	}
}

// RegisterLegacyAminoCodec registers the necessary x/ibc transfer interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	// do nothing
}

// RegisterInterfaces register the ibc transfer module interfaces to protobuf
// Any.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgRetryRefund{},
		&MsgClaimRefund{},
	)
	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgUpdateParamsResponse{},
		&MsgRetryRefundResponse{},
		&MsgClaimRefundResponse{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// ModuleCdc references the global x/ibc-transfer module codec. Note, the codec
// should ONLY be used in certain instances of tests and for JSON encoding.
//
// The actual codec used for serialization should be provided to x/ibc transfer and
// defined at the application level.
var ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
