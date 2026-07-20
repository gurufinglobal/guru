package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgUpsertTask{},
		&MsgRemoveTask{},
	)
	registry.RegisterImplementations((*txtypes.TxExtensionOptionI)(nil),
		&OracleProposalPayload{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
