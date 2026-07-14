package cli

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestUpdateAdminCommandBuildsMessage(t *testing.T) {
	from := sdk.AccAddress([]byte("01234567890123456789"))
	originalGetContext := getClientTxContext
	originalBroadcast := generateOrBroadcastTxCLI
	var captured []sdk.Msg
	getClientTxContext = func(*cobra.Command) (client.Context, error) {
		return client.Context{FromAddress: from}, nil
	}
	generateOrBroadcastTxCLI = func(_ client.Context, _ *pflag.FlagSet, msgs ...sdk.Msg) error {
		captured = append(captured, msgs...)
		return nil
	}
	t.Cleanup(func() {
		getClientTxContext = originalGetContext
		generateOrBroadcastTxCLI = originalBroadcast
	})

	registered, _, err := GetTxCmd().Find([]string{"update-admin"})
	require.NoError(t, err)
	require.Equal(t, "update-admin", registered.Name())

	cmd := CmdUpdateAdmin()
	require.NoError(t, cmd.RunE(cmd, []string{"old-admin", "new-admin"}))
	require.Equal(t, []sdk.Msg{&bexv1.MsgUpdateAdmin{
		Moderator:       from.String(),
		OldAdminAddress: "old-admin",
		NewAdminAddress: "new-admin",
	}}, captured)
}
