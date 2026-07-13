package types

import (
	"testing"

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
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
		&bexv1.MsgAddReserveDepositor{},
		&bexv1.MsgRemoveReserveDepositor{},
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
		&bexv1.MsgAddReserveDepositorResponse{},
		&bexv1.MsgRemoveReserveDepositorResponse{},
		&bexv1.MsgDepositReserveResponse{},
		&bexv1.MsgWithdrawReserveResponse{},
		&bexv1.MsgWithdrawFeesResponse{},
	}
	for _, response := range responses {
		require.NoError(t, registry.EnsureRegistered(response))
	}
}

func TestMsgSignerAnnotationsBindAuthorityFields(t *testing.T) {
	tests := []struct {
		name   string
		msg    proto.Message
		signer string
	}{
		{name: "register admin", msg: &bexv1.MsgRegisterAdmin{}, signer: "moderator"},
		{name: "remove admin", msg: &bexv1.MsgRemoveAdmin{}, signer: "moderator"},
		{name: "register exchange", msg: &bexv1.MsgRegisterExchange{}, signer: "admin_address"},
		{name: "update exchange", msg: &bexv1.MsgUpdateExchange{}, signer: "admin_address"},
		{name: "delete exchange", msg: &bexv1.MsgDeleteExchange{}, signer: "admin_address"},
		{name: "add reserve depositor", msg: &bexv1.MsgAddReserveDepositor{}, signer: "admin_address"},
		{name: "remove reserve depositor", msg: &bexv1.MsgRemoveReserveDepositor{}, signer: "admin_address"},
		{name: "deposit reserve", msg: &bexv1.MsgDepositReserve{}, signer: "sender"},
		{name: "withdraw reserve", msg: &bexv1.MsgWithdrawReserve{}, signer: "admin_address"},
		{name: "withdraw fees", msg: &bexv1.MsgWithdrawFees{}, signer: "admin_address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := tt.msg.ProtoReflect().Descriptor()
			opts, ok := desc.Options().(*descriptorpb.MessageOptions)
			require.True(t, ok)
			require.True(t, proto.HasExtension(opts, msgv1.E_Signer), "%s has no signer annotation", desc.FullName())

			ext := proto.GetExtension(opts, msgv1.E_Signer)
			signers, ok := ext.([]string)
			require.True(t, ok, "unexpected signer extension type %T", ext)
			require.Equal(t, []string{tt.signer}, signers)

			field := desc.Fields().ByName(protoreflect.Name(tt.signer))
			require.NotNil(t, field, "signer field %q is missing from %s", tt.signer, desc.FullName())
			require.Equal(t, protoreflect.StringKind, field.Kind())
		})
	}
}
