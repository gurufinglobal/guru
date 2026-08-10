package types

import (
	"fmt"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/protoadapt"
)

func TestMessageSigners(t *testing.T) {
	signingContext, err := txsigning.NewContext(txsigning.Options{
		AddressCodec:          signerOnlyAddressCodec{},
		ValidatorAddressCodec: signerOnlyAddressCodec{},
	})
	require.NoError(t, err)

	tests := []struct {
		method string
		msg    sdk.Msg
		signer string
	}{
		{
			method: "RegisterAdmin",
			msg: &MsgRegisterAdmin{
				Moderator:    "signer-moderator",
				AdminAddress: "non-signer-admin",
			},
			signer: "signer-moderator",
		},
		{
			method: "UpdateAdmin",
			msg: &MsgUpdateAdmin{
				Moderator:       "signer-moderator",
				OldAdminAddress: "non-signer-old-admin",
				NewAdminAddress: "non-signer-new-admin",
			},
			signer: "signer-moderator",
		},
		{
			method: "RemoveAdmin",
			msg: &MsgRemoveAdmin{
				Moderator:    "signer-moderator",
				AdminAddress: "non-signer-admin",
			},
			signer: "signer-moderator",
		},
		{
			method: "RegisterExchange",
			msg: &MsgRegisterExchange{
				BexAdminAddress:      "signer-bex-admin",
				ExchangeAdminAddress: "non-signer-exchange-admin",
			},
			signer: "signer-bex-admin",
		},
		{
			method: "UpdateExchange",
			msg: &MsgUpdateExchange{
				AdminAddress:     "signer-current-exchange-admin",
				ExchangeId:       7,
				Patch:            &ExchangeUpdatePatch{},
				ExpectedRevision: 3,
			},
			signer: "signer-current-exchange-admin",
		},
		{
			method: "DeleteExchange",
			msg: &MsgDeleteExchange{
				AdminAddress: "signer-exchange-admin",
				ExchangeId:   7,
			},
			signer: "signer-exchange-admin",
		},
		{
			method: "AddReserveDepositor",
			msg: &MsgAddReserveDepositor{
				AdminAddress:     "signer-exchange-admin",
				ExchangeId:       7,
				DepositorAddress: "non-signer-depositor",
			},
			signer: "signer-exchange-admin",
		},
		{
			method: "RemoveReserveDepositor",
			msg: &MsgRemoveReserveDepositor{
				AdminAddress:     "signer-exchange-admin",
				ExchangeId:       7,
				DepositorAddress: "non-signer-depositor",
			},
			signer: "signer-exchange-admin",
		},
		{
			method: "DepositReserve",
			msg: &MsgDepositReserve{
				Sender:     "signer-depositor",
				ExchangeId: 7,
			},
			signer: "signer-depositor",
		},
		{
			method: "WithdrawReserve",
			msg: &MsgWithdrawReserve{
				AdminAddress: "signer-exchange-admin",
				ExchangeId:   7,
				Recipient:    "non-signer-recipient",
			},
			signer: "signer-exchange-admin",
		},
		{
			method: "WithdrawFees",
			msg: &MsgWithdrawFees{
				AdminAddress: "signer-exchange-admin",
				ExchangeId:   7,
				Recipient:    "non-signer-recipient",
			},
			signer: "signer-exchange-admin",
		},
	}

	covered := make(map[string]struct{}, len(tests))
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			signers, err := signingContext.GetSigners(protoadapt.MessageV2Of(tc.msg))
			require.NoError(t, err)
			require.Equal(t, [][]byte{[]byte(tc.signer)}, signers)
		})
		covered[tc.method] = struct{}{}
	}

	require.Equal(t, len(Msg_serviceDesc.Methods), len(tests), "every Msg RPC must have a signer regression case")
	for _, method := range Msg_serviceDesc.Methods {
		require.Contains(t, covered, method.MethodName, "missing signer regression for Msg/%s", method.MethodName)
	}
}

// signerOnlyAddressCodec deliberately rejects non-signer target addresses.
// Successful signer extraction therefore proves that recipient, depositor,
// and replacement-admin addresses are not transaction signers.
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
