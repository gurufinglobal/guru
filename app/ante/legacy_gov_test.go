package ante

import (
	"testing"

	errorsmod "cosmossdk.io/errors"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

func TestRejectLegacyGovMessages(t *testing.T) {
	tests := []struct {
		name string
		msg  sdk.Msg
	}{
		{
			name: "legacy submit proposal",
			msg:  &govv1beta1.MsgSubmitProposal{},
		},
		{
			name: "exec legacy content",
			msg:  &govv1.MsgExecLegacyContent{},
		},
		{
			name: "nested legacy submit proposal",
			msg: ptr(authztypes.NewMsgExec(
				sdk.AccAddress("grantee_______________"),
				[]sdk.Msg{&govv1beta1.MsgSubmitProposal{}},
			)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectLegacyGovMessages(testTx{msgs: []sdk.Msg{tt.msg}})
			require.Error(t, err)
			require.True(t, errorsmod.IsOf(err, sdkerrors.ErrInvalidRequest), err)
		})
	}
}

func TestRejectLegacyGovMessagesAllowsCurrentGovProposal(t *testing.T) {
	err := rejectLegacyGovMessages(testTx{msgs: []sdk.Msg{&govv1.MsgSubmitProposal{
		Messages: []*codectypes.Any{},
	}}})
	require.NoError(t, err)
}

type testTx struct {
	msgs []sdk.Msg
}

func (t testTx) GetMsgs() []sdk.Msg {
	return t.msgs
}

func (t testTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func ptr[T any](value T) *T {
	return &value
}
