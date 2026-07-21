package types

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

func TestRegisterInterfaces(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(registry)

	msgs := []sdk.Msg{
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
	}
	for _, msg := range msgs {
		require.NoError(t, registry.EnsureRegistered(msg))
	}

	responses := []tx.MsgResponse{
		&MsgRegisterAdminResponse{},
		&MsgUpdateAdminResponse{},
		&MsgRemoveAdminResponse{},
		&MsgRegisterExchangeResponse{},
		&MsgUpdateExchangeResponse{},
		&MsgDeleteExchangeResponse{},
		&MsgAddReserveDepositorResponse{},
		&MsgRemoveReserveDepositorResponse{},
		&MsgDepositReserveResponse{},
		&MsgWithdrawReserveResponse{},
		&MsgWithdrawFeesResponse{},
	}
	for _, response := range responses {
		require.NoError(t, registry.EnsureRegistered(response))
	}
}

func TestMsgServiceDescriptorCoversRegisteredMessages(t *testing.T) {
	messages := map[string]sdk.Msg{
		"RegisterAdmin":          &MsgRegisterAdmin{},
		"UpdateAdmin":            &MsgUpdateAdmin{},
		"RemoveAdmin":            &MsgRemoveAdmin{},
		"RegisterExchange":       &MsgRegisterExchange{},
		"UpdateExchange":         &MsgUpdateExchange{},
		"DeleteExchange":         &MsgDeleteExchange{},
		"AddReserveDepositor":    &MsgAddReserveDepositor{},
		"RemoveReserveDepositor": &MsgRemoveReserveDepositor{},
		"DepositReserve":         &MsgDepositReserve{},
		"WithdrawReserve":        &MsgWithdrawReserve{},
		"WithdrawFees":           &MsgWithdrawFees{},
	}

	registry := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(registry)
	require.Len(t, messages, len(Msg_serviceDesc.Methods))
	for _, method := range Msg_serviceDesc.Methods {
		msg, ok := messages[method.MethodName]
		require.True(t, ok, "missing concrete message for Msg/%s", method.MethodName)
		require.NoError(t, registry.EnsureRegistered(msg))
	}
}
