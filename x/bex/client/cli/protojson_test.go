package cli

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/stretchr/testify/require"
)

func TestDecodeStrictJSONAcceptsBexProtoJSON(t *testing.T) {
	register := &bexv1.MsgRegisterExchange{}
	require.NoError(t, decodeStrictJSON(
		`{"denom_a":"agxn","exchange_admin_address":"exchange-owner"}`,
		register,
	))
	require.Equal(t, "agxn", register.GetDenomA())
	require.Equal(t, "exchange-owner", register.GetExchangeAdminAddress())

	patch := &bexv1.ExchangeUpdatePatch{}
	require.NoError(t, decodeStrictJSON(
		`{"new_admin_address":"new-owner","fee_bps_a_to_b":9}`,
		patch,
	))
	require.Equal(t, "new-owner", patch.GetNewAdminAddress().GetValue())
	require.Equal(t, uint32(9), patch.GetFeeBpsAToB().GetValue())
}

func TestDecodeStrictJSONRejectsUnknownAndTrailingJSON(t *testing.T) {
	for _, raw := range []string{
		`{"new_admin_address":"new-owner","unknown_field":true}`,
		`{"new_admin_address":"new-owner"} {}`,
	} {
		t.Run(raw, func(t *testing.T) {
			err := decodeStrictJSON(raw, &bexv1.ExchangeUpdatePatch{})
			require.Error(t, err)
		})
	}
}

func TestCmdUpdateExchangeAcceptsStandardProtoJSONWrappers(t *testing.T) {
	from := sdk.AccAddress(bytes.Repeat([]byte{0x45}, 20))
	captured := installTxMocks(t, from, nil)

	err := CmdUpdateExchange().RunE(
		CmdUpdateExchange(),
		[]string{"7", `{"new_admin_address":"new-owner","fee_bps_a_to_b":9}`, "3"},
	)

	require.NoError(t, err)
	require.Len(t, *captured, 1)
	msg := (*captured)[0].(*bexv1.MsgUpdateExchange)
	require.Equal(t, from.String(), msg.GetAdminAddress())
	require.Equal(t, "new-owner", msg.GetPatch().GetNewAdminAddress().GetValue())
	require.Equal(t, uint32(9), msg.GetPatch().GetFeeBpsAToB().GetValue())
}
