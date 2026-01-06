package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino = codec.NewLegacyAmino()
	// ModuleCdc is the codec for the oracle module.
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterLegacyAminoCodec(amino)
	cryptocodec.RegisterCrypto(amino)
	amino.Seal()
}

// RegisterLegacyAminoCodec registers the necessary interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateModeratorAddress{}, "oracle/UpdateModeratorAddress", nil)
	cdc.RegisterConcrete(&MsgRegisterOracleRequest{}, "oracle/RegisterOracleRequest", nil)
	cdc.RegisterConcrete(&MsgUpdateOracleRequest{}, "oracle/UpdateOracleRequest", nil)
	cdc.RegisterConcrete(&MsgSubmitOracleReport{}, "oracle/SubmitOracleReport", nil)
	cdc.RegisterConcrete(&MsgAddToWhitelist{}, "oracle/AddToWhitelist", nil)
	cdc.RegisterConcrete(&MsgRemoveFromWhitelist{}, "oracle/RemoveFromWhitelist", nil)
}

// RegisterInterfaces registers the module's interface types.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateModeratorAddress{},
		&MsgRegisterOracleRequest{},
		&MsgUpdateOracleRequest{},
		&MsgSubmitOracleReport{},
		&MsgAddToWhitelist{},
		&MsgRemoveFromWhitelist{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
