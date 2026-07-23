package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterAdmin{},
		&MsgUpdateAdmin{},
		&MsgRemoveAdmin{},
		&MsgRegisterExchange{},
		&MsgUpdateExchange{},
		&MsgDeleteExchange{},
		&MsgAddReserveDepositor{},
		&MsgRemoveReserveDepositor{},
		&MsgDepositReserve{},
		&MsgWithdrawReserve{},
		&MsgWithdrawFees{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
