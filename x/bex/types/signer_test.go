package types

import (
	"fmt"
	"strings"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestMessageSigners(t *testing.T) {
	registry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: protoregistry.GlobalFiles,
		SigningOptions: txsigning.Options{
			AddressCodec:          signerOnlyAddressCodec{},
			ValidatorAddressCodec: signerOnlyAddressCodec{},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name   string
		msg    proto.Message
		signer string
	}{
		{
			name: "register admin",
			msg: &bexv1.MsgRegisterAdmin{
				Moderator:    "signer-moderator",
				AdminAddress: "non-signer-admin",
			},
			signer: "signer-moderator",
		},
		{
			name: "update admin",
			msg: &bexv1.MsgUpdateAdmin{
				Moderator:       "signer-moderator",
				OldAdminAddress: "non-signer-old-admin",
				NewAdminAddress: "non-signer-new-admin",
			},
			signer: "signer-moderator",
		},
		{
			name: "remove admin",
			msg: &bexv1.MsgRemoveAdmin{
				Moderator:    "signer-moderator",
				AdminAddress: "non-signer-admin",
			},
			signer: "signer-moderator",
		},
		{
			name: "register exchange",
			msg: &bexv1.MsgRegisterExchange{
				BexAdminAddress:      "signer-bex-admin",
				ExchangeAdminAddress: "non-signer-exchange-admin",
			},
			signer: "signer-bex-admin",
		},
		{
			name: "update exchange",
			msg: &bexv1.MsgUpdateExchange{
				AdminAddress: "signer-current-exchange-admin",
				ExchangeId:   7,
				Patch: &bexv1.ExchangeUpdatePatch{
					NewAdminAddress: wrapperspb.String("non-signer-new-exchange-admin"),
				},
				ExpectedRevision: 3,
			},
			signer: "signer-current-exchange-admin",
		},
		{
			name: "delete exchange",
			msg: &bexv1.MsgDeleteExchange{
				AdminAddress: "signer-exchange-admin",
				ExchangeId:   7,
			},
			signer: "signer-exchange-admin",
		},
		{
			name: "add reserve depositor",
			msg: &bexv1.MsgAddReserveDepositor{
				AdminAddress:     "signer-exchange-admin",
				ExchangeId:       7,
				DepositorAddress: "non-signer-depositor",
			},
			signer: "signer-exchange-admin",
		},
		{
			name: "remove reserve depositor",
			msg: &bexv1.MsgRemoveReserveDepositor{
				AdminAddress:     "signer-exchange-admin",
				ExchangeId:       7,
				DepositorAddress: "non-signer-depositor",
			},
			signer: "signer-exchange-admin",
		},
		{
			name: "deposit reserve",
			msg: &bexv1.MsgDepositReserve{
				Sender:     "signer-depositor",
				ExchangeId: 7,
			},
			signer: "signer-depositor",
		},
		{
			name: "withdraw reserve",
			msg: &bexv1.MsgWithdrawReserve{
				AdminAddress: "signer-exchange-admin",
				ExchangeId:   7,
				Recipient:    "non-signer-recipient",
			},
			signer: "signer-exchange-admin",
		},
		{
			name: "withdraw fees",
			msg: &bexv1.MsgWithdrawFees{
				AdminAddress: "signer-exchange-admin",
				ExchangeId:   7,
				Recipient:    "non-signer-recipient",
			},
			signer: "signer-exchange-admin",
		},
	}

	covered := make(map[string]struct{}, len(tests))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signers, err := registry.SigningContext().GetSigners(tc.msg)
			require.NoError(t, err)
			require.Equal(t, [][]byte{[]byte(tc.signer)}, signers)
		})
		covered[string(tc.msg.ProtoReflect().Descriptor().FullName())] = struct{}{}
	}

	msgService := bexv1.File_guru_bex_v1_tx_proto.Services().ByName("Msg")
	require.NotNil(t, msgService)
	require.Equal(t, msgService.Methods().Len(), len(tests), "every Msg RPC must have a signer regression case")
	for i := 0; i < msgService.Methods().Len(); i++ {
		inputName := string(msgService.Methods().Get(i).Input().FullName())
		require.Contains(t, covered, inputName, "missing signer regression for %s", inputName)
	}
}

// signerOnlyAddressCodec deliberately rejects non-signer target addresses.
// Successful signer extraction therefore proves that recipient, depositor,
// replacement-admin, and nested patch addresses are not transaction signers.
type signerOnlyAddressCodec struct{}

func (signerOnlyAddressCodec) StringToBytes(text string) ([]byte, error) {
	if !strings.HasPrefix(text, "signer-") {
		return nil, fmt.Errorf("unexpected signer address %q", text)
	}
	return []byte(text), nil
}

func (signerOnlyAddressCodec) BytesToString(bz []byte) (string, error) {
	return string(bz), nil
}
