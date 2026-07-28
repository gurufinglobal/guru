package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

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

type queryClientCapture struct {
	types.QueryClient
	discountRequest *types.QueryDiscountRequest
}

func (c *queryClientCapture) Discount(
	_ context.Context,
	req *types.QueryDiscountRequest,
	_ ...grpc.CallOption,
) (*types.QueryDiscountResponse, error) {
	c.discountRequest = req
	return &types.QueryDiscountResponse{}, nil
}

func TestGlobalDiscountQueryCommand(t *testing.T) {
	originalGetCtx := getClientQueryContext
	originalNewClient := newQueryClient
	originalPrint := printQueryResponse
	capture := &queryClientCapture{}
	getClientQueryContext = func(*cobra.Command) (client.Context, error) { return client.Context{}, nil }
	newQueryClient = func(client.Context) types.QueryClient { return capture }
	printQueryResponse = func(client.Context, printableProto) error { return nil }
	t.Cleanup(func() {
		getClientQueryContext = originalGetCtx
		newQueryClient = originalNewClient
		printQueryResponse = originalPrint
	})

	cmd := GetCmdQueryDiscount()
	require.NoError(t, cmd.Args(cmd, []string{globalPolicySelector}))
	require.NoError(t, cmd.RunE(cmd, []string{globalPolicySelector}))
	require.NotNil(t, capture.discountRequest)
	require.Empty(t, capture.discountRequest.Address)
}

func TestReadPageRequest(t *testing.T) {
	nextKey := []byte{0x01, 0x02, 0x03}
	cmd := GetCmdQueryDiscounts()
	require.NoError(t, cmd.Flags().Set(flags.FlagPageKey, base64.StdEncoding.EncodeToString(nextKey)))
	require.NoError(t, cmd.Flags().Set(flags.FlagLimit, "7"))

	pageRequest, err := readPageRequest(cmd)
	require.NoError(t, err)
	require.Equal(t, nextKey, pageRequest.Key)
	require.Equal(t, uint64(7), pageRequest.Limit)

	invalid := GetCmdQueryDiscounts()
	require.NoError(t, invalid.Flags().Set(flags.FlagPageKey, "not-base64"))
	_, err = readPageRequest(invalid)
	require.Error(t, err)
}

func TestRemoveDiscountsCommandBuildsGlobalAndMsgTypeRequests(t *testing.T) {
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
	var got *types.MsgRemoveDiscounts
	generateOrBroadcastTxCLI = func(_ client.Context, _ *pflag.FlagSet, msgs ...sdk.Msg) error {
		require.Len(t, msgs, 1)
		got = msgs[0].(*types.MsgRemoveDiscounts)
		return nil
	}
	t.Cleanup(func() {
		getClientTxContext = originalGetCtx
		generateOrBroadcastTxCLI = originalGenerate
	})

	msgType := "/cosmos.bank.v1beta1.MsgSend"
	account := "0x0202020202020202020202020202020202020202"
	for _, tc := range []struct {
		name string
		args []string
		want types.MsgRemoveDiscounts
	}{
		{
			name: "global policy",
			args: []string{globalPolicySelector},
			want: types.MsgRemoveDiscounts{ModeratorAddress: clientCtx.FromAddress.String()},
		},
		{
			name: "message type",
			args: []string{account, "bank", msgType},
			want: types.MsgRemoveDiscounts{
				ModeratorAddress: clientCtx.FromAddress.String(),
				Address:          account,
				Module:           "bank",
				MsgType:          msgType,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got = nil
			cmd := NewRemoveDiscountsTxCmd()
			require.NoError(t, cmd.Args(cmd, tc.args))
			require.NoError(t, cmd.RunE(cmd, tc.args))
			require.Equal(t, tc.want, *got)
		})
	}

	got = nil
	cmd := NewRemoveDiscountsTxCmd()
	args := []string{account, "", msgType}
	require.NoError(t, cmd.Args(cmd, args))
	require.Error(t, cmd.RunE(cmd, args))
	require.Nil(t, got)
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
