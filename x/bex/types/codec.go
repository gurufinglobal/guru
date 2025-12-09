package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino = codec.NewLegacyAmino()

	// ModuleCdc references the global xbex module codec. Note, the codec should
	// ONLY be used in certain instances of tests and for JSON encoding.
	//
	// The actual codec used for serialization should be provided to modules/xbex and
	// defined at the application level.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	// AminoCdc is a amino codec created to support amino JSON compatible msgs.
	AminoCdc = codec.NewAminoCodec(amino)
)

const (
	// Amino names
	registerAdminName    = "guru/MsgRegisterAdmin"
	removeAdminName      = "guru/MsgRemoveAdmin"
	registerExchangeName = "guru/MsgRegisterExchange"
	updateExchangeName   = "guru/MsgUpdateExchange"
	updateRatemeterName  = "guru/MsgUpdateRatemeter"
	withdrawFeesName     = "guru/MsgWithdrawFees"
	changeModeratorName  = "guru/MsgChangeBexModerator"
)

// NOTE: This is required for the GetSignBytes function
func init() {
	RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

// RegisterInterfaces register implementations
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterAdmin{},
		&MsgRemoveAdmin{},
		&MsgRegisterExchange{},
		&MsgUpdateExchange{},
		&MsgUpdateRatemeter{},
		&MsgWithdrawFees{},
		&MsgChangeBexModerator{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// RegisterLegacyAminoCodec registers the necessary x/bex interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterAdmin{}, registerAdminName, nil)
	cdc.RegisterConcrete(&MsgRemoveAdmin{}, removeAdminName, nil)
	cdc.RegisterConcrete(&MsgRegisterExchange{}, registerExchangeName, nil)
	cdc.RegisterConcrete(&MsgUpdateExchange{}, updateExchangeName, nil)
	cdc.RegisterConcrete(&MsgUpdateRatemeter{}, updateRatemeterName, nil)
	cdc.RegisterConcrete(&MsgWithdrawFees{}, withdrawFeesName, nil)
	cdc.RegisterConcrete(&MsgChangeBexModerator{}, changeModeratorName, nil)
}
