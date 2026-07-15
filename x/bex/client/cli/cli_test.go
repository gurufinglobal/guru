package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestGetTxCmdIncludesBexCommands(t *testing.T) {
	cmd := GetTxCmd()

	for _, name := range []string{
		"register-admin",
		"update-admin",
		"remove-admin",
		"register-exchange",
		"update-exchange",
		"delete-exchange",
		"add-reserve-depositor",
		"remove-reserve-depositor",
		"deposit-reserve",
		"withdraw-reserve",
		"withdraw-fees",
	} {
		_, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
	}
}

func TestGetQueryCmdIncludesBexCommands(t *testing.T) {
	cmd := GetQueryCmd()

	for _, name := range []string{
		"exchange",
		"exchanges",
		"exchanges-by-exchange-admin",
		"is-bex-admin",
		"reserve-depositors",
		"is-reserve-depositor",
		"collected-fees",
		"locked-fees",
		"available-fees",
		"pending-liabilities",
		"volume-window",
		"quote",
	} {
		_, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
	}
}

func TestParseExchangeIDAndCoins(t *testing.T) {
	exchangeID, coins, err := parseExchangeIDAndCoins("7", "123agxn")

	require.NoError(t, err)
	require.Equal(t, uint64(7), exchangeID)
	require.Len(t, coins, 1)
	require.Equal(t, "agxn", coins[0].GetDenom())
	require.Equal(t, "123", coins[0].GetAmount())
}

func TestParseExchangeIDAndCoinsRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		idRaw    string
		coinsRaw string
	}{
		{name: "invalid id", idRaw: "abc", coinsRaw: "1agxn"},
		{name: "invalid coins", idRaw: "7", coinsRaw: "agxn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseExchangeIDAndCoins(tc.idRaw, tc.coinsRaw)
			require.Error(t, err)
		})
	}
}

func TestParseExchangeIDAndDirection(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		direction bexv1.SwapDirection
	}{
		{raw: "a-to-b", direction: bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B},
		{raw: "A_TO_B", direction: bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B},
		{raw: "1", direction: bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B},
		{raw: "b-to-a", direction: bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A},
		{raw: "B_TO_A", direction: bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A},
		{raw: "2", direction: bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			exchangeID, direction, err := parseExchangeIDAndDirection("9", tc.raw)

			require.NoError(t, err)
			require.Equal(t, uint64(9), exchangeID)
			require.Equal(t, tc.direction, direction)
		})
	}
}

func TestParseExchangeIDAndDirectionRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		name         string
		idRaw        string
		directionRaw string
	}{
		{name: "invalid id", idRaw: "abc", directionRaw: "a-to-b"},
		{name: "invalid direction", idRaw: "9", directionRaw: "sideways"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseExchangeIDAndDirection(tc.idRaw, tc.directionRaw)
			require.Error(t, err)
		})
	}
}

func TestReadPulsarPageRequest(t *testing.T) {
	cmd := CmdQueryExchanges()
	require.NoError(t, cmd.Flags().Set(flags.FlagLimit, "25"))
	require.NoError(t, cmd.Flags().Set(flags.FlagOffset, "50"))
	require.NoError(t, cmd.Flags().Set(flags.FlagCountTotal, "true"))
	require.NoError(t, cmd.Flags().Set(flags.FlagReverse, "true"))

	pageReq, err := readPulsarPageRequest(cmd)

	require.NoError(t, err)
	require.Equal(t, uint64(25), pageReq.GetLimit())
	require.Equal(t, uint64(50), pageReq.GetOffset())
	require.True(t, pageReq.GetCountTotal())
	require.True(t, pageReq.GetReverse())
}

func TestQueryCommandsBuildRequestsAndPropagateErrors(t *testing.T) {
	queryErr := errors.New("query failed")
	printErr := errors.New("print failed")
	ctxErr := errors.New("context failed")

	queryCommands := []struct {
		name   string
		method string
		cmd    func() *cobra.Command
		args   []string
	}{
		{name: "exchange", method: "exchange", cmd: CmdQueryExchange, args: []string{"7"}},
		{name: "exchanges", method: "exchanges", cmd: CmdQueryExchanges},
		{name: "exchanges by exchange admin", method: "exchanges-by-exchange-admin", cmd: CmdQueryExchangesByExchangeAdmin, args: []string{"exchange-admin"}},
		{name: "is BEX admin", method: "is-bex-admin", cmd: CmdQueryIsBexAdmin, args: []string{"bex-admin"}},
		{name: "reserve depositors", method: "reserve-depositors", cmd: CmdQueryReserveDepositors, args: []string{"7"}},
		{name: "is reserve depositor", method: "is-reserve-depositor", cmd: CmdQueryIsReserveDepositor, args: []string{"7", "depositor"}},
		{name: "collected fees", method: "collected-fees", cmd: CmdQueryCollectedFees, args: []string{"7"}},
		{name: "locked fees", method: "locked-fees", cmd: CmdQueryLockedFees, args: []string{"7"}},
		{name: "available fees", method: "available-fees", cmd: CmdQueryAvailableFees, args: []string{"7"}},
		{name: "pending liabilities", method: "pending-liabilities", cmd: CmdQueryPendingLiabilities, args: []string{"7"}},
		{name: "volume window", method: "volume-window", cmd: CmdQueryVolumeWindow, args: []string{"7", "a-to-b"}},
		{name: "quote", method: "quote", cmd: CmdQueryQuote, args: []string{"7", "agxn", "10"}},
	}

	for _, tc := range queryCommands {
		t.Run(tc.name, func(t *testing.T) {
			mock := installQueryMocks(t, nil, nil, nil)
			cmd := tc.cmd()
			require.NoError(t, cmd.RunE(cmd, tc.args))
			require.Equal(t, tc.method, mock.method)
		})
	}

	t.Run("representative context error", func(t *testing.T) {
		installQueryMocks(t, ctxErr, nil, nil)
		cmd := CmdQueryExchange()
		require.ErrorIs(t, cmd.RunE(cmd, []string{"7"}), ctxErr)
	})
	t.Run("representative query error", func(t *testing.T) {
		installQueryMocks(t, nil, queryErr, nil)
		cmd := CmdQueryExchange()
		require.ErrorIs(t, cmd.RunE(cmd, []string{"7"}), queryErr)
	})
	t.Run("representative print error", func(t *testing.T) {
		installQueryMocks(t, nil, nil, printErr)
		cmd := CmdQueryExchange()
		require.ErrorIs(t, cmd.RunE(cmd, []string{"7"}), printErr)
	})

	installQueryMocks(t, nil, nil, nil)
	require.Error(t, CmdQueryExchange().RunE(CmdQueryExchange(), []string{"bad"}))
	require.Error(t, CmdQueryCollectedFees().RunE(CmdQueryCollectedFees(), []string{"bad"}))
	require.Error(t, CmdQueryPendingLiabilities().RunE(CmdQueryPendingLiabilities(), []string{"bad"}))
	require.Error(t, CmdQueryReserveDepositors().RunE(CmdQueryReserveDepositors(), []string{"bad"}))
	require.Error(t, CmdQueryIsReserveDepositor().RunE(CmdQueryIsReserveDepositor(), []string{"bad", "depositor"}))
	require.Error(t, CmdQueryVolumeWindow().RunE(CmdQueryVolumeWindow(), []string{"7", "sideways"}))
	require.Error(t, CmdQueryQuote().RunE(CmdQueryQuote(), []string{"bad", "agxn", "10"}))

	badPageKey := CmdQueryExchanges()
	require.NoError(t, badPageKey.Flags().Set(flags.FlagPageKey, "not-base64"))
	require.Error(t, badPageKey.RunE(badPageKey, nil))
	badPageOffset := CmdQueryExchangesByExchangeAdmin()
	require.NoError(t, badPageOffset.Flags().Set(flags.FlagPage, "2"))
	require.NoError(t, badPageOffset.Flags().Set(flags.FlagOffset, "1"))
	require.Error(t, badPageOffset.RunE(badPageOffset, []string{"admin"}))
}

func TestExplicitAdminQueryArguments(t *testing.T) {
	t.Run("exchange admin owner index", func(t *testing.T) {
		mock := installQueryMocks(t, nil, nil, nil)
		cmd := CmdQueryExchangesByExchangeAdmin()

		require.NoError(t, cmd.RunE(cmd, []string{"exchange-admin"}))
		require.Equal(t, "exchange-admin", mock.exchangeAdminAddress)
	})

	t.Run("BEX registrar registry", func(t *testing.T) {
		mock := installQueryMocks(t, nil, nil, nil)
		cmd := CmdQueryIsBexAdmin()

		require.NoError(t, cmd.RunE(cmd, []string{"bex-admin"}))
		require.Equal(t, "bex-admin", mock.bexAdminAddress)
	})
}

func TestQueryExchangesIncludesDeletedWhenRequested(t *testing.T) {
	mock := installQueryMocks(t, nil, nil, nil)
	cmd := CmdQueryExchanges()
	require.NoError(t, cmd.Flags().Set(flagIncludeDeleted, "true"))

	require.NoError(t, cmd.RunE(cmd, nil))
	require.True(t, mock.includeDeleted)
}

func TestTxCommandsBuildMessagesAndPropagateErrors(t *testing.T) {
	ctxErr := errors.New("context failed")
	broadcastErr := errors.New("broadcast failed")
	from := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))

	txCommands := []struct {
		name      string
		cmd       func() *cobra.Command
		args      []string
		assertMsg func(*testing.T, sdk.Msg)
	}{
		{
			name: "register admin",
			cmd:  CmdRegisterAdmin,
			args: []string{"admin"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgRegisterAdmin)
				require.Equal(t, from.String(), typed.GetModerator())
				require.Equal(t, "admin", typed.GetAdminAddress())
			},
		},
		{
			name: "remove admin",
			cmd:  CmdRemoveAdmin,
			args: []string{"admin"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgRemoveAdmin)
				require.Equal(t, from.String(), typed.GetModerator())
				require.Equal(t, "admin", typed.GetAdminAddress())
			},
		},
		{
			name: "register exchange",
			cmd:  CmdRegisterExchange,
			args: []string{`{"denom_a":"agxn","exchange_admin_address":"exchange-owner"}`},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgRegisterExchange)
				require.Equal(t, from.String(), typed.GetBexAdminAddress())
				require.Equal(t, "exchange-owner", typed.GetExchangeAdminAddress())
				require.Equal(t, "agxn", typed.GetDenomA())
			},
		},
		{
			name: "update exchange",
			cmd:  CmdUpdateExchange,
			args: []string{"7", `{"fee_bps_a_to_b":9}`, "3"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgUpdateExchange)
				require.Equal(t, from.String(), typed.GetAdminAddress())
				require.Equal(t, uint64(7), typed.GetExchangeId())
				require.Equal(t, uint64(3), typed.GetExpectedRevision())
			},
		},
		{
			name: "delete exchange",
			cmd:  CmdDeleteExchange,
			args: []string{"7"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgDeleteExchange)
				require.Equal(t, from.String(), typed.GetAdminAddress())
				require.Equal(t, uint64(7), typed.GetExchangeId())
			},
		},
		{
			name: "add reserve depositor",
			cmd:  CmdAddReserveDepositor,
			args: []string{"7", "depositor"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgAddReserveDepositor)
				require.Equal(t, from.String(), typed.GetAdminAddress())
				require.Equal(t, uint64(7), typed.GetExchangeId())
				require.Equal(t, "depositor", typed.GetDepositorAddress())
			},
		},
		{
			name: "remove reserve depositor",
			cmd:  CmdRemoveReserveDepositor,
			args: []string{"7", "depositor"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgRemoveReserveDepositor)
				require.Equal(t, from.String(), typed.GetAdminAddress())
				require.Equal(t, uint64(7), typed.GetExchangeId())
				require.Equal(t, "depositor", typed.GetDepositorAddress())
			},
		},
		{
			name: "deposit reserve",
			cmd:  CmdDepositReserve,
			args: []string{"7", "1agxn"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgDepositReserve)
				require.Equal(t, from.String(), typed.GetSender())
				require.Equal(t, uint64(7), typed.GetExchangeId())
				require.Len(t, typed.GetAmount(), 1)
			},
		},
		{
			name: "withdraw reserve",
			cmd:  CmdWithdrawReserve,
			args: []string{"7", "1agxn", "recipient"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgWithdrawReserve)
				require.Equal(t, from.String(), typed.GetAdminAddress())
				require.Equal(t, "recipient", typed.GetRecipient())
			},
		},
		{
			name: "withdraw fees",
			cmd:  CmdWithdrawFees,
			args: []string{"7", "recipient", "1agxn"},
			assertMsg: func(t *testing.T, msg sdk.Msg) {
				typed := msg.(*bexv1.MsgWithdrawFees)
				require.Equal(t, from.String(), typed.GetAdminAddress())
				require.Equal(t, "recipient", typed.GetRecipient())
			},
		},
	}

	for _, tc := range txCommands {
		t.Run(tc.name, func(t *testing.T) {
			captured := installTxMocks(t, from, nil, nil)
			cmd := tc.cmd()
			require.NoError(t, cmd.RunE(cmd, tc.args))
			require.Len(t, *captured, 1)
			tc.assertMsg(t, (*captured)[0])
		})
	}

	t.Run("representative context error", func(t *testing.T) {
		installTxMocks(t, from, ctxErr, nil)
		cmd := CmdRegisterAdmin()
		require.ErrorIs(t, cmd.RunE(cmd, []string{"admin"}), ctxErr)
	})
	t.Run("representative broadcast error", func(t *testing.T) {
		installTxMocks(t, from, nil, broadcastErr)
		cmd := CmdRegisterAdmin()
		require.ErrorIs(t, cmd.RunE(cmd, []string{"admin"}), broadcastErr)
	})

	installTxMocks(t, from, nil, nil)
	require.Error(t, CmdRegisterExchange().RunE(CmdRegisterExchange(), []string{"{"}))
	require.Error(t, CmdUpdateExchange().RunE(CmdUpdateExchange(), []string{"bad", `{}`, "3"}))
	require.Error(t, CmdUpdateExchange().RunE(CmdUpdateExchange(), []string{"7", `{}`, "bad"}))
	require.Error(t, CmdUpdateExchange().RunE(CmdUpdateExchange(), []string{"7", `{`, "3"}))
	require.Error(t, CmdDeleteExchange().RunE(CmdDeleteExchange(), []string{"bad"}))
	require.Error(t, CmdAddReserveDepositor().RunE(CmdAddReserveDepositor(), []string{"bad", "depositor"}))
	require.Error(t, CmdRemoveReserveDepositor().RunE(CmdRemoveReserveDepositor(), []string{"bad", "depositor"}))
	require.Error(t, CmdDepositReserve().RunE(CmdDepositReserve(), []string{"bad", "1agxn"}))
	require.Error(t, CmdWithdrawReserve().RunE(CmdWithdrawReserve(), []string{"7", "bad", "recipient"}))
	require.Error(t, CmdWithdrawFees().RunE(CmdWithdrawFees(), []string{"7", "recipient", "bad"}))
	require.Error(t, CmdRegisterExchange().RunE(CmdRegisterExchange(), []string{`{"denom_a":"agxn","denom_typo":"gxusd"}`}))
	require.Error(t, CmdRegisterExchange().RunE(CmdRegisterExchange(), []string{`{} {}`}))
}

func installQueryMocks(t *testing.T, contextErr, queryErr, printErr error) *mockQueryClient {
	t.Helper()

	originalGetCtx := getClientQueryContext
	originalNewClient := newQueryClient
	originalPrint := printProto
	mock := &mockQueryClient{err: queryErr}
	getClientQueryContext = func(*cobra.Command) (client.Context, error) { return client.Context{}, contextErr }
	newQueryClient = func(grpc.ClientConnInterface) bexv1.QueryClient { return mock }
	printProto = func(client.Context, printableProto) error { return printErr }
	t.Cleanup(func() {
		getClientQueryContext = originalGetCtx
		newQueryClient = originalNewClient
		printProto = originalPrint
	})
	return mock
}

func installTxMocks(t *testing.T, from sdk.AccAddress, contextErr, broadcastErr error) *[]sdk.Msg {
	t.Helper()

	originalGetCtx := getClientTxContext
	originalBroadcast := generateOrBroadcastTxCLI
	captured := []sdk.Msg{}
	getClientTxContext = func(*cobra.Command) (client.Context, error) {
		return client.Context{FromAddress: from}, contextErr
	}
	generateOrBroadcastTxCLI = func(_ client.Context, _ *pflag.FlagSet, msgs ...sdk.Msg) error {
		captured = append(captured, msgs...)
		return broadcastErr
	}
	t.Cleanup(func() {
		getClientTxContext = originalGetCtx
		generateOrBroadcastTxCLI = originalBroadcast
	})
	return &captured
}

type mockQueryClient struct {
	method               string
	exchangeAdminAddress string
	bexAdminAddress      string
	includeDeleted       bool
	err                  error
}

func (m *mockQueryClient) Exchange(context.Context, *bexv1.QueryExchangeRequest, ...grpc.CallOption) (*bexv1.QueryExchangeResponse, error) {
	m.method = "exchange"
	return &bexv1.QueryExchangeResponse{}, m.err
}

func (m *mockQueryClient) Exchanges(_ context.Context, req *bexv1.QueryExchangesRequest, _ ...grpc.CallOption) (*bexv1.QueryExchangesResponse, error) {
	m.method = "exchanges"
	m.includeDeleted = req.GetIncludeDeleted()
	return &bexv1.QueryExchangesResponse{}, m.err
}

func (m *mockQueryClient) ExchangesByExchangeAdmin(_ context.Context, req *bexv1.QueryExchangesByExchangeAdminRequest, _ ...grpc.CallOption) (*bexv1.QueryExchangesByExchangeAdminResponse, error) {
	m.method = "exchanges-by-exchange-admin"
	m.exchangeAdminAddress = req.GetExchangeAdminAddress()
	return &bexv1.QueryExchangesByExchangeAdminResponse{}, m.err
}

func (m *mockQueryClient) IsBexAdmin(_ context.Context, req *bexv1.QueryIsBexAdminRequest, _ ...grpc.CallOption) (*bexv1.QueryIsBexAdminResponse, error) {
	m.method = "is-bex-admin"
	m.bexAdminAddress = req.GetBexAdminAddress()
	return &bexv1.QueryIsBexAdminResponse{}, m.err
}

func (m *mockQueryClient) ReserveDepositors(context.Context, *bexv1.QueryReserveDepositorsRequest, ...grpc.CallOption) (*bexv1.QueryReserveDepositorsResponse, error) {
	m.method = "reserve-depositors"
	return &bexv1.QueryReserveDepositorsResponse{}, m.err
}

func (m *mockQueryClient) IsReserveDepositor(context.Context, *bexv1.QueryIsReserveDepositorRequest, ...grpc.CallOption) (*bexv1.QueryIsReserveDepositorResponse, error) {
	m.method = "is-reserve-depositor"
	return &bexv1.QueryIsReserveDepositorResponse{}, m.err
}

func (m *mockQueryClient) CollectedFees(context.Context, *bexv1.QueryFeesRequest, ...grpc.CallOption) (*bexv1.QueryFeesResponse, error) {
	m.method = "collected-fees"
	return &bexv1.QueryFeesResponse{}, m.err
}

func (m *mockQueryClient) LockedFees(context.Context, *bexv1.QueryFeesRequest, ...grpc.CallOption) (*bexv1.QueryFeesResponse, error) {
	m.method = "locked-fees"
	return &bexv1.QueryFeesResponse{}, m.err
}

func (m *mockQueryClient) AvailableFees(context.Context, *bexv1.QueryFeesRequest, ...grpc.CallOption) (*bexv1.QueryFeesResponse, error) {
	m.method = "available-fees"
	return &bexv1.QueryFeesResponse{}, m.err
}

func (m *mockQueryClient) PendingLiabilities(context.Context, *bexv1.QueryPendingLiabilitiesRequest, ...grpc.CallOption) (*bexv1.QueryPendingLiabilitiesResponse, error) {
	m.method = "pending-liabilities"
	return &bexv1.QueryPendingLiabilitiesResponse{}, m.err
}

func (m *mockQueryClient) VolumeWindow(context.Context, *bexv1.QueryVolumeWindowRequest, ...grpc.CallOption) (*bexv1.QueryVolumeWindowResponse, error) {
	m.method = "volume-window"
	return &bexv1.QueryVolumeWindowResponse{}, m.err
}

func (m *mockQueryClient) QuoteSwap(context.Context, *bexv1.QueryQuoteSwapRequest, ...grpc.CallOption) (*bexv1.QueryQuoteSwapResponse, error) {
	m.method = "quote"
	return &bexv1.QueryQuoteSwapResponse{}, m.err
}
