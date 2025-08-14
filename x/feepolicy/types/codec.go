// Copyright 2024 Gurufin
// This file is part of the Gurux packages.

// Gurux is proprietary software: you are not allowed to redistribute, modify,
// or use it for any purpose without explicit permission from Gurufin. Any unauthorized
// use of the software, including but not limited to copying, distribution, or modification,
// is strictly prohibited.
//
// The Gurux packages are provided "as is", without any warranty or guarantee of
// any kind, either express or implied, including but not limited to the implied
// warranties of merchantability, fitness for a particular purpose, or non-infringement.
// Gurux does not take responsibility for any damage, loss, or inconvenience caused by the use
// of this software.
//
// You may not reverse-engineer, decompile, or disassemble the software without express permission
// from Gurux. The software may only be used in accordance with the terms set by Gurux, and all rights
// to the software remain solely with Gurux.
//
// For more information, please contact Gurufin at: <contact@gurufin.com>
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino = codec.NewLegacyAmino()

	// ModuleCdc references the global x/feepolicy module codec. Note, the codec should
	// ONLY be used in certain instances of tests and for JSON encoding.
	//
	// The actual codec used for serialization should be provided to modules/x/feepolicy and
	// defined at the application level.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	// AminoCdc is a amino codec created to support amino JSON compatible msgs.
	AminoCdc = codec.NewAminoCodec(amino)
)

const (
	// Amino names
	registerDiscountsName = "guru/MsgRegisterDiscounts"
	removeDiscountsName   = "guru/MsgRemoveDiscounts"
	changeModeratorName   = "guru/MsgChangeModerator"
)

// NOTE: This is required for the GetSignBytes function
func init() {
	RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

// RegisterInterfaces register implementations
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterDiscounts{},
		&MsgRemoveDiscounts{},
		&MsgChangeModerator{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// RegisterLegacyAminoCodec registers the necessary x/feepolicy interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterDiscounts{}, registerDiscountsName, nil)
	cdc.RegisterConcrete(&MsgRemoveDiscounts{}, removeDiscountsName, nil)
	cdc.RegisterConcrete(&MsgChangeModerator{}, changeModeratorName, nil)
}
