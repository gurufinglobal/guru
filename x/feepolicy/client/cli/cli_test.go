package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

func TestCommandTree(t *testing.T) {
	for _, tc := range []struct {
		newCommand func() *cobra.Command
		names      []string
	}{
		{GetTxCmd, []string{"change_moderator", "register_discounts", "remove_discounts"}},
		{GetQueryCmd, []string{"discount", "discounts", "moderator_address"}},
	} {
		cmd := tc.newCommand()
		require.Equal(t, types.ModuleName, cmd.Name())
		require.True(t, cmd.DisableFlagParsing)
		require.Len(t, cmd.Commands(), len(tc.names))
		for _, name := range tc.names {
			found, _, err := cmd.Find([]string{name})
			require.NoError(t, err)
			require.Equal(t, name, found.Name())
		}
	}
}

func TestRegisterCommandAcceptsInlineAndFileJSON(t *testing.T) {
	encoding := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	types.RegisterInterfaces(encoding.InterfaceRegistry)
	clientCtx := client.Context{
		FromAddress:       sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20)),
		Codec:             encoding.Codec,
		InterfaceRegistry: encoding.InterfaceRegistry,
		TxConfig:          encoding.TxConfig,
	}

	originalGetCtx := getClientTxContext
	originalGenerate := generateOrBroadcastTxCLI
	getClientTxContext = func(*cobra.Command) (client.Context, error) { return clientCtx, nil }
	var got *types.MsgRegisterDiscounts
	generateOrBroadcastTxCLI = func(_ client.Context, _ *pflag.FlagSet, msgs ...sdk.Msg) error {
		require.Len(t, msgs, 1)
		got = msgs[0].(*types.MsgRegisterDiscounts)
		return nil
	}
	t.Cleanup(func() {
		getClientTxContext = originalGetCtx
		generateOrBroadcastTxCLI = originalGenerate
	})

	discountsJSON := `{"discounts":[{"address":"0x0202020202020202020202020202020202020202","modules":[{"module":"bank","discounts":[{"discount_type":"percent","msg_type":"/cosmos.bank.v1beta1.MsgSend","amount":"25.000000000000000000"}]}]}]}`
	jsonPath := filepath.Join(t.TempDir(), "discounts.json")
	require.NoError(t, os.WriteFile(jsonPath, []byte(discountsJSON), 0o600))

	for _, input := range []string{discountsJSON, jsonPath} {
		t.Run(filepath.Base(input), func(t *testing.T) {
			got = nil
			cmd := NewRegisterDiscountsTxCmd()
			require.NoError(t, cmd.RunE(cmd, []string{input}))
			require.Equal(t, clientCtx.FromAddress.String(), got.ModeratorAddress)
			require.Equal(t, "bank", got.Discounts[0].Modules[0].Module)
		})
	}
}
