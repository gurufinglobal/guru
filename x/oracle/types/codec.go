package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = amino
)

func init() {
	RegisterLegacyAminoCodec(amino)
	cryptocodec.RegisterCrypto(amino)
	amino.Seal()
}

// RegisterLegacyAminoCodec registers the necessary x/oracle interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterOracleRequestDoc{}, "oracle/RegisterOracleRequestDoc", nil)
	cdc.RegisterConcrete(&MsgUpdateOracleRequestDoc{}, "oracle/UpdateOracleRequestDoc", nil)
	cdc.RegisterConcrete(&MsgSubmitOracleData{}, "oracle/SubmitOracleData", nil)
	cdc.RegisterConcrete(&MsgUpdateModeratorAddress{}, "oracle/UpdateModeratorAddress", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "oracle/UpdateParams", nil)
}

// RegisterInterfaces registers the x/oracle interfaces types with the interface registry
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterOracleRequestDoc{},
		&MsgUpdateOracleRequestDoc{},
		&MsgSubmitOracleData{},
		&MsgUpdateModeratorAddress{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
