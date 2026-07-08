package types

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/stretchr/testify/require"
)

func TestRegisterInterfaces(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(registry)

	msgs := []sdk.Msg{
		&bexv1.MsgRegisterAdmin{},
		&bexv1.MsgRemoveAdmin{},
		&bexv1.MsgRegisterExchange{},
		&bexv1.MsgUpdateExchange{},
		&bexv1.MsgDeleteExchange{},
		&bexv1.MsgDepositReserve{},
		&bexv1.MsgWithdrawReserve{},
		&bexv1.MsgWithdrawFees{},
	}
	for _, msg := range msgs {
		require.NoError(t, registry.EnsureRegistered(msg))
	}

	responses := []tx.MsgResponse{
		&bexv1.MsgRegisterAdminResponse{},
		&bexv1.MsgRemoveAdminResponse{},
		&bexv1.MsgRegisterExchangeResponse{},
		&bexv1.MsgUpdateExchangeResponse{},
		&bexv1.MsgDeleteExchangeResponse{},
		&bexv1.MsgDepositReserveResponse{},
		&bexv1.MsgWithdrawReserveResponse{},
		&bexv1.MsgWithdrawFeesResponse{},
	}
	for _, response := range responses {
		require.NoError(t, registry.EnsureRegistered(response))
	}
}
