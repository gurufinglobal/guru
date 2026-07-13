package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
)

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&bexv1.MsgRegisterAdmin{},
		&bexv1.MsgRemoveAdmin{},
		&bexv1.MsgRegisterExchange{},
		&bexv1.MsgUpdateExchange{},
		&bexv1.MsgDeleteExchange{},
		&bexv1.MsgAddReserveDepositor{},
		&bexv1.MsgRemoveReserveDepositor{},
		&bexv1.MsgDepositReserve{},
		&bexv1.MsgWithdrawReserve{},
		&bexv1.MsgWithdrawFees{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&bexv1.MsgRegisterAdminResponse{},
		&bexv1.MsgRemoveAdminResponse{},
		&bexv1.MsgRegisterExchangeResponse{},
		&bexv1.MsgUpdateExchangeResponse{},
		&bexv1.MsgDeleteExchangeResponse{},
		&bexv1.MsgAddReserveDepositorResponse{},
		&bexv1.MsgRemoveReserveDepositorResponse{},
		&bexv1.MsgDepositReserveResponse{},
		&bexv1.MsgWithdrawReserveResponse{},
		&bexv1.MsgWithdrawFeesResponse{},
	)
}
