package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
)

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&constitutionv1.MsgUpdateParams{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&constitutionv1.MsgUpdateParamsResponse{},
	)
}
