package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino = codec.NewLegacyAmino()

	// ModuleCdc references the global feeproxy module codec. Note, the codec should
	// ONLY be used in certain instances of tests and for JSON encoding.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	// AminoCdc is an amino codec created to support amino JSON compatible msgs.
	// Kept for compatibility with generic signing (e.g. GetSignBytes).
	AminoCdc = codec.NewAminoCodec(amino) //nolint:staticcheck
)

const (
	registerAdminName        = "guru/feeproxy/MsgRegisterAdmin"
	updateFeePercentageName  = "guru/feeproxy/MsgUpdateFeePercentage"
	updateReserveAddressName = "guru/feeproxy/MsgUpdateReserveAddress"
)

// NOTE: This is required for the GetSignBytes function.
func init() {
	RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

// RegisterInterfaces registers the client interfaces to protobuf Any.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterAdmin{},
		&MsgUpdateFeePercentage{},
		&MsgUpdateReserveAddress{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// RegisterLegacyAminoCodec registers concrete types on the provided LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterAdmin{}, registerAdminName, nil)
	cdc.RegisterConcrete(&MsgUpdateFeePercentage{}, updateFeePercentageName, nil)
	cdc.RegisterConcrete(&MsgUpdateReserveAddress{}, updateReserveAddressName, nil)
}
