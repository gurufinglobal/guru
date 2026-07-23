package app

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	evmmodule "github.com/cosmos/evm/x/vm"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/ethereum/go-ethereum/common"

	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bexmodule "github.com/gurufinglobal/guru/v3/x/bex"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var configureBexEVMOnce sync.Once

func configureBexEVM() {
	configureBexEVMOnce.Do(func() {
		evmmodule.SetGlobalConfigVariables(evmtypes.EvmCoinInfo{
			Denom:         appparams.BaseDenom,
			ExtendedDenom: appparams.BaseDenom,
			DisplayDenom:  appparams.DisplayDenom,
			Decimals:      18,
		})
	})
}

func TestBexAllMsgAndQueryServicesExecuteThroughRuntimeRouters(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	ctx := testApp.NewNextBlockContext(cmtproto.Header{
		ChainID: appparams.SDKChainID,
		Height:  1,
		Time:    time.Unix(1_700_000_000, 0),
	})
	require.NoError(t, testApp.BankKeeper.SetParams(ctx, banktypes.DefaultParams()))
	testApp.IBCKeeper.ChannelKeeper.SetChannel(ctx, "transwap", "channel-0", channeltypes.Channel{State: channeltypes.OPEN})
	testApp.IBCKeeper.ChannelKeeper.SetChannel(ctx, "transwap", "channel-1", channeltypes.Channel{State: channeltypes.OPEN})

	moderator := wiringAddress(t, 0x31)
	oldBexAdmin := wiringAddress(t, 0x32)
	newBexAdmin := wiringAddress(t, 0x33)
	exchangeAdmin := wiringAddress(t, 0x34)
	depositor := sdk.AccAddress(repeatedByteAddress(0x35))
	depositorString, err := testApp.AccountKeeper.AddressCodec().BytesToString(depositor)
	require.NoError(t, err)
	recipient := sdk.AccAddress(repeatedByteAddress(0x36))
	recipientString, err := testApp.AccountKeeper.AddressCodec().BytesToString(recipient)
	require.NoError(t, err)
	require.NoError(t, testApp.ConstitutionKeeper.SetModeratorAddress(ctx, moderator))

	funding := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 10))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, funding))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, depositor, funding))

	executedMsgs := map[string]struct{}{}
	executeBexRuntimeMsg(t, testApp, ctx, "RegisterAdmin", &bextypes.MsgRegisterAdmin{
		Moderator:    moderator,
		AdminAddress: oldBexAdmin,
	}, &bextypes.MsgRegisterAdminResponse{}, executedMsgs)
	executeBexRuntimeMsg(t, testApp, ctx, "UpdateAdmin", &bextypes.MsgUpdateAdmin{
		Moderator:       moderator,
		OldAdminAddress: oldBexAdmin,
		NewAdminAddress: newBexAdmin,
	}, &bextypes.MsgUpdateAdminResponse{}, executedMsgs)

	registerResponse := &bextypes.MsgRegisterExchangeResponse{}
	executeBexRuntimeMsg(t, testApp, ctx, "RegisterExchange", &bextypes.MsgRegisterExchange{
		BexAdminAddress:           newBexAdmin,
		ExchangeAdminAddress:      exchangeAdmin,
		DenomA:                    appparams.BaseDenom,
		PortA:                     "transwap",
		ChannelA:                  "channel-0",
		DenomB:                    "gxusd",
		PortB:                     "transwap",
		ChannelB:                  "channel-1",
		OracleSymbolAToB:          "AGXN/GXUSD",
		OracleSymbolBToA:          "GXUSD/AGXN",
		FeeBpsAToB:                25,
		FeeBpsBToA:                25,
		LimitAToB:                 "1000",
		LimitBToA:                 "1000",
		VolumeCapAToB:             "1000",
		VolumeCapBToA:             "1000",
		Status:                    bextypes.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
		Metadata:                  map[string]string{"venue": "runtime-router-test"},
		VolumeEpochSeconds:        86_400,
		MaxOracleStalenessSeconds: 60,
	}, registerResponse, executedMsgs)
	require.Equal(t, uint64(1), registerResponse.GetExchangeId())
	require.NotEmpty(t, registerResponse.GetReserveAddress())
	exchangeID := registerResponse.GetExchangeId()

	executeBexRuntimeMsg(t, testApp, ctx, "AddReserveDepositor", &bextypes.MsgAddReserveDepositor{
		AdminAddress:     exchangeAdmin,
		ExchangeId:       exchangeID,
		DepositorAddress: depositorString,
	}, &bextypes.MsgAddReserveDepositorResponse{}, executedMsgs)
	executeBexRuntimeMsg(t, testApp, ctx, "DepositReserve", &bextypes.MsgDepositReserve{
		Sender:     depositorString,
		ExchangeId: exchangeID,
		Amount:     sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 10)),
	}, &bextypes.MsgDepositReserveResponse{}, executedMsgs)

	require.NoError(t, testApp.OracleKeeper.SetLatestValue(ctx, &oracletypes.OracleValue{
		Symbol:        "AGXN/GXUSD",
		ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Value:         "2",
		BlockHeight:   ctx.BlockHeight(),
		BlockTimeUnix: ctx.BlockTime().Unix(),
	}))
	require.NoError(t, testApp.BexKeeper.CollectFee(ctx, exchangeID, sdk.NewInt64Coin(appparams.BaseDenom, 4)))
	require.NoError(t, testApp.BexKeeper.LockExchangeFee(ctx, exchangeID, sdk.NewInt64Coin(appparams.BaseDenom, 1)))
	require.NoError(t, testApp.BexKeeper.RecordVolumeWindow(
		ctx,
		exchangeID,
		bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
		sdkmath.NewInt(7),
	))

	executedQueries := map[string]struct{}{}
	exchangeResponse := &bextypes.QueryExchangeResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "Exchange", &bextypes.QueryExchangeRequest{ExchangeId: exchangeID}, exchangeResponse, executedQueries)
	require.Equal(t, exchangeID, exchangeResponse.GetExchange().GetId())
	require.Equal(t, bextypes.ExchangeStatus_EXCHANGE_STATUS_ACTIVE, exchangeResponse.GetExchange().GetStatus())

	exchangesResponse := &bextypes.QueryExchangesResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "Exchanges", &bextypes.QueryExchangesRequest{}, exchangesResponse, executedQueries)
	require.Len(t, exchangesResponse.GetExchanges(), 1)
	require.Equal(t, exchangeID, exchangesResponse.GetExchanges()[0].GetId())

	adminExchangesResponse := &bextypes.QueryExchangesByExchangeAdminResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "ExchangesByExchangeAdmin", &bextypes.QueryExchangesByExchangeAdminRequest{
		ExchangeAdminAddress: exchangeAdmin,
	}, adminExchangesResponse, executedQueries)
	require.Len(t, adminExchangesResponse.GetExchanges(), 1)
	require.Equal(t, exchangeID, adminExchangesResponse.GetExchanges()[0].GetId())

	isBexAdminResponse := &bextypes.QueryIsBexAdminResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "IsBexAdmin", &bextypes.QueryIsBexAdminRequest{
		BexAdminAddress: newBexAdmin,
	}, isBexAdminResponse, executedQueries)
	require.True(t, isBexAdminResponse.GetIsBexAdmin())

	depositorsResponse := &bextypes.QueryReserveDepositorsResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "ReserveDepositors", &bextypes.QueryReserveDepositorsRequest{
		ExchangeId: exchangeID,
	}, depositorsResponse, executedQueries)
	require.Equal(t, []string{depositorString}, depositorsResponse.GetDepositors())

	isDepositorResponse := &bextypes.QueryIsReserveDepositorResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "IsReserveDepositor", &bextypes.QueryIsReserveDepositorRequest{
		ExchangeId:       exchangeID,
		DepositorAddress: depositorString,
	}, isDepositorResponse, executedQueries)
	require.True(t, isDepositorResponse.GetIsReserveDepositor())

	collectedResponse := &bextypes.QueryFeesResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "CollectedFees", &bextypes.QueryFeesRequest{ExchangeId: exchangeID}, collectedResponse, executedQueries)
	requireBexLedgerCoin(t, collectedResponse.GetLedger(), appparams.BaseDenom, "4")

	lockedResponse := &bextypes.QueryFeesResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "LockedFees", &bextypes.QueryFeesRequest{ExchangeId: exchangeID}, lockedResponse, executedQueries)
	requireBexLedgerCoin(t, lockedResponse.GetLedger(), appparams.BaseDenom, "1")

	availableResponse := &bextypes.QueryFeesResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "AvailableFees", &bextypes.QueryFeesRequest{ExchangeId: exchangeID}, availableResponse, executedQueries)
	requireBexLedgerCoin(t, availableResponse.GetLedger(), appparams.BaseDenom, "3")

	pendingLiabilitiesResponse := &bextypes.QueryPendingLiabilitiesResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "PendingLiabilities", &bextypes.QueryPendingLiabilitiesRequest{
		ExchangeId: exchangeID,
	}, pendingLiabilitiesResponse, executedQueries)
	require.NotNil(t, pendingLiabilitiesResponse.GetLedger())
	require.Empty(t, pendingLiabilitiesResponse.GetLedger().GetCoins())

	volumeResponse := &bextypes.QueryVolumeWindowResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "VolumeWindow", &bextypes.QueryVolumeWindowRequest{
		ExchangeId: exchangeID,
		Direction:  bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
	}, volumeResponse, executedQueries)
	require.Equal(t, "7", volumeResponse.GetWindow().GetAmount())
	require.Equal(t, "1000", volumeResponse.GetCap())

	quoteResponse := &bextypes.QueryQuoteSwapResponse{}
	executeBexRuntimeQuery(t, testApp, ctx, "QuoteSwap", &bextypes.QueryQuoteSwapRequest{
		ExchangeId: exchangeID,
		InputDenom: appparams.BaseDenom,
		AmountIn:   "101",
	}, quoteResponse, executedQueries)
	require.Equal(t, bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B, quoteResponse.GetQuote().GetDirection())
	require.Equal(t, "1", quoteResponse.GetQuote().GetFeeAmount())
	require.Equal(t, "100", quoteResponse.GetQuote().GetNetAmountIn())
	require.Equal(t, "200", quoteResponse.GetQuote().GetAmountOut())
	require.Equal(t, "7", quoteResponse.GetQuote().GetVolumeUsed())

	require.NoError(t, testApp.BexKeeper.ReleaseExchangeFee(ctx, exchangeID, sdk.NewInt64Coin(appparams.BaseDenom, 1)))
	executeBexRuntimeMsg(t, testApp, ctx, "WithdrawFees", &bextypes.MsgWithdrawFees{
		AdminAddress: exchangeAdmin,
		ExchangeId:   exchangeID,
		Amount:       sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 4)),
		Recipient:    recipientString,
	}, &bextypes.MsgWithdrawFeesResponse{}, executedMsgs)

	updateResponse := &bextypes.MsgUpdateExchangeResponse{}
	executeBexRuntimeMsg(t, testApp, ctx, "UpdateExchange", &bextypes.MsgUpdateExchange{
		AdminAddress:     exchangeAdmin,
		ExchangeId:       exchangeID,
		ExpectedRevision: 1,
		Patch: &bextypes.ExchangeUpdatePatch{Status: &bextypes.ExchangeStatusPatch{
			Status: bextypes.ExchangeStatus_EXCHANGE_STATUS_INACTIVE,
		}},
	}, updateResponse, executedMsgs)
	require.Equal(t, uint64(2), updateResponse.GetRevision())

	executeBexRuntimeMsg(t, testApp, ctx, "RemoveReserveDepositor", &bextypes.MsgRemoveReserveDepositor{
		AdminAddress:     exchangeAdmin,
		ExchangeId:       exchangeID,
		DepositorAddress: depositorString,
	}, &bextypes.MsgRemoveReserveDepositorResponse{}, executedMsgs)
	executeBexRuntimeMsg(t, testApp, ctx, "WithdrawReserve", &bextypes.MsgWithdrawReserve{
		AdminAddress: exchangeAdmin,
		ExchangeId:   exchangeID,
		Amount:       sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 6)),
		Recipient:    recipientString,
	}, &bextypes.MsgWithdrawReserveResponse{}, executedMsgs)
	executeBexRuntimeMsg(t, testApp, ctx, "DeleteExchange", &bextypes.MsgDeleteExchange{
		AdminAddress: exchangeAdmin,
		ExchangeId:   exchangeID,
	}, &bextypes.MsgDeleteExchangeResponse{}, executedMsgs)
	executeBexRuntimeMsg(t, testApp, ctx, "RemoveAdmin", &bextypes.MsgRemoveAdmin{
		Moderator:    moderator,
		AdminAddress: newBexAdmin,
	}, &bextypes.MsgRemoveAdminResponse{}, executedMsgs)

	deletedExchange, err := testApp.BexKeeper.GetExchange(ctx, exchangeID)
	require.NoError(t, err)
	require.Equal(t, bextypes.ExchangeStatus_EXCHANGE_STATUS_DELETED, deletedExchange.GetStatus())
	isOldBexAdmin, err := testApp.BexKeeper.IsAdmin(ctx, oldBexAdmin)
	require.NoError(t, err)
	require.False(t, isOldBexAdmin)
	isNewBexAdmin, err := testApp.BexKeeper.IsAdmin(ctx, newBexAdmin)
	require.NoError(t, err)
	require.False(t, isNewBexAdmin)
	isDepositor, err := testApp.BexKeeper.IsReserveDepositor(ctx, exchangeID, depositorString)
	require.NoError(t, err)
	require.False(t, isDepositor)
	require.Equal(t, int64(10), testApp.BankKeeper.GetBalance(ctx, recipient, appparams.BaseDenom).Amount.Int64())
	require.NoError(t, testApp.BexKeeper.AssertInvariants(ctx))
	require.NoError(t, testApp.BexKeeper.AssertFeeSolvency(ctx))
	requireBexServiceMethodsExecuted(t, reflect.TypeOf((*bextypes.MsgServer)(nil)).Elem(), executedMsgs)
	requireBexServiceMethodsExecuted(t, reflect.TypeOf((*bextypes.QueryServer)(nil)).Elem(), executedQueries)
}

func TestBexReserveRestrictionIsWiredAcrossBankAndEVM(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	require.Contains(t, testApp.ModuleManager.Modules, bextypes.ModuleName)
	require.NotNil(t, testApp.EVMKeeper)
	configureBexEVM()

	ctx := testApp.NewNextBlockContext(cmtproto.Header{
		ChainID: appparams.SDKChainID,
		Height:  1,
		Time:    time.Unix(1_700_000_000, 0),
	})
	require.NoError(t, testApp.BankKeeper.SetParams(ctx, banktypes.DefaultParams()))
	moderator := wiringAddress(t, 0x22)
	require.NoError(t, testApp.ConstitutionKeeper.SetModeratorAddress(ctx, moderator))

	admin := sdk.AccAddress(repeatedByteAddress(0x61))
	adminString, err := testApp.AccountKeeper.AddressCodec().BytesToString(admin)
	require.NoError(t, err)
	require.NoError(t, testApp.BexKeeper.RegisterAdmin(ctx, moderator, adminString))

	exchange, err := testApp.BexKeeper.RegisterExchange(ctx, &bextypes.MsgRegisterExchange{
		BexAdminAddress:           adminString,
		ExchangeAdminAddress:      adminString,
		DenomA:                    "asset-a",
		PortA:                     "transwap",
		ChannelA:                  "channel-0",
		DenomB:                    "asset-b",
		PortB:                     "transwap",
		ChannelB:                  "channel-1",
		OracleSymbolAToB:          "ASSET-A/ASSET-B",
		OracleSymbolBToA:          "ASSET-B/ASSET-A",
		Status:                    bextypes.ExchangeStatus_EXCHANGE_STATUS_INACTIVE,
		VolumeEpochSeconds:        86_400,
		MaxOracleStalenessSeconds: 60,
	})
	require.NoError(t, err)

	reserve := testApp.BexKeeper.GetReserveAddress(ctx, exchange.GetId())
	require.NotNil(t, testApp.AccountKeeper.GetAccount(ctx, reserve))

	funding := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 10))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, funding))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, admin, funding))

	err = testApp.BankKeeper.SendCoins(ctx, admin, reserve, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)))
	require.ErrorIs(t, err, bextypes.ErrDirectReserveTransfer)

	require.NoError(t, testApp.BexKeeper.DepositReserve(
		ctx,
		adminString,
		exchange.GetId(),
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2)),
	))
	require.Equal(t, int64(2), testApp.BankKeeper.GetBalance(ctx, reserve, appparams.BaseDenom).Amount.Int64())

	depositor := sdk.AccAddress(repeatedByteAddress(0x63))
	depositorString, err := testApp.AccountKeeper.AddressCodec().BytesToString(depositor)
	require.NoError(t, err)
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2))))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		minttypes.ModuleName,
		depositor,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2)),
	))
	require.NoError(t, testApp.BexKeeper.AddReserveDepositor(ctx, adminString, exchange.GetId(), depositorString))
	require.ErrorIs(
		t,
		testApp.BankKeeper.SendCoins(ctx, depositor, reserve, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1))),
		bextypes.ErrDirectReserveTransfer,
	)
	require.NoError(t, testApp.BexKeeper.DepositReserve(
		ctx,
		depositorString,
		exchange.GetId(),
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
	))
	require.Equal(t, int64(3), testApp.BankKeeper.GetBalance(ctx, reserve, appparams.BaseDenom).Amount.Int64())
	require.NoError(t, testApp.BexKeeper.RemoveReserveDepositor(ctx, adminString, exchange.GetId(), depositorString))
	require.ErrorIs(
		t,
		testApp.BexKeeper.DepositReserve(
			ctx,
			depositorString,
			exchange.GetId(),
			sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
		),
		bextypes.ErrUnauthorizedReserveDepositor,
	)

	// EVM native-value writes commit through UncheckedSetBalance rather than the
	// normal bank send pipeline. The app adapter therefore rejects both reserve
	// increases and decreases; only BEX keeper operations may mutate custody.
	err = testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(reserve), uint256.NewInt(4))
	require.ErrorIs(t, err, bextypes.ErrDirectReserveTransfer)
	require.Equal(t, int64(3), testApp.BankKeeper.GetBalance(ctx, reserve, appparams.BaseDenom).Amount.Int64())

	nonReserve := sdk.AccAddress(repeatedByteAddress(0x62))
	require.NoError(t, testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(nonReserve), uint256.NewInt(3)))
	require.Equal(t, int64(3), testApp.BankKeeper.GetBalance(ctx, nonReserve, appparams.BaseDenom).Amount.Int64())
	require.NoError(t, testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(nonReserve), uint256.NewInt(1)))
	require.NoError(t, testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(nonReserve), uint256.NewInt(1)))
	require.Equal(t, int64(1), testApp.BankKeeper.GetBalance(ctx, nonReserve, appparams.BaseDenom).Amount.Int64())

	err = testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(reserve), uint256.NewInt(1))
	require.ErrorIs(t, err, bextypes.ErrDirectReserveTransfer)
	require.Equal(t, int64(3), testApp.BankKeeper.GetBalance(ctx, reserve, appparams.BaseDenom).Amount.Int64())
}

func TestBexRegistrationReclaimsPrecreatedKeylessVestingReserve(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	ctx := testApp.NewNextBlockContext(cmtproto.Header{
		ChainID: appparams.SDKChainID,
		Height:  1,
		Time:    time.Unix(1_700_000_000, 0),
	})
	require.NoError(t, testApp.BankKeeper.SetParams(ctx, banktypes.DefaultParams()))
	moderator := wiringAddress(t, 0x23)
	require.NoError(t, testApp.ConstitutionKeeper.SetModeratorAddress(ctx, moderator))
	admin := sdk.AccAddress(repeatedByteAddress(0x71))
	adminString, err := testApp.AccountKeeper.AddressCodec().BytesToString(admin)
	require.NoError(t, err)
	require.NoError(t, testApp.BexKeeper.RegisterAdmin(ctx, moderator, adminString))

	funding := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, funding))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, admin, funding))

	prospectiveReserve := testApp.BexKeeper.GetReserveAddress(ctx, 1)
	prospectiveReserveString, err := testApp.AccountKeeper.AddressCodec().BytesToString(prospectiveReserve)
	require.NoError(t, err)
	vestingMsg := &vestingtypes.MsgCreatePermanentLockedAccount{
		FromAddress: adminString,
		ToAddress:   prospectiveReserveString,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
	}
	vestingHandler := testApp.MsgServiceRouter().Handler(vestingMsg)
	require.NotNil(t, vestingHandler)
	_, err = vestingHandler(ctx, vestingMsg)
	require.NoError(t, err)
	_, isBaseBefore := testApp.AccountKeeper.GetAccount(ctx, prospectiveReserve).(*authtypes.BaseAccount)
	require.False(t, isBaseBefore)

	exchange, err := testApp.BexKeeper.RegisterExchange(ctx, &bextypes.MsgRegisterExchange{
		BexAdminAddress:           adminString,
		ExchangeAdminAddress:      adminString,
		DenomA:                    "asset-a",
		PortA:                     "transwap",
		ChannelA:                  "channel-0",
		DenomB:                    "asset-b",
		PortB:                     "transwap",
		ChannelB:                  "channel-1",
		OracleSymbolAToB:          "ASSET-A/ASSET-B",
		OracleSymbolBToA:          "ASSET-B/ASSET-A",
		Status:                    bextypes.ExchangeStatus_EXCHANGE_STATUS_INACTIVE,
		VolumeEpochSeconds:        86_400,
		MaxOracleStalenessSeconds: 60,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), exchange.GetId())
	recovered, ok := testApp.AccountKeeper.GetAccount(ctx, prospectiveReserve).(*authtypes.BaseAccount)
	require.True(t, ok)
	require.Nil(t, recovered.GetPubKey())
	require.Equal(t, int64(1), testApp.BankKeeper.GetBalance(ctx, prospectiveReserve, appparams.BaseDenom).Amount.Int64())
	require.ErrorIs(
		t,
		testApp.BankKeeper.SendCoins(ctx, admin, prospectiveReserve, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1))),
		bextypes.ErrDirectReserveTransfer,
	)
}

func TestBexFeeCustodyBoundariesAcrossBankAndEVM(t *testing.T) {
	configureBexEVM()
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	ctx := testApp.NewNextBlockContext(cmtproto.Header{
		ChainID: appparams.SDKChainID,
		Height:  1,
		Time:    time.Unix(1_700_000_000, 0),
	})
	require.NoError(t, testApp.BankKeeper.SetParams(ctx, banktypes.DefaultParams()))
	moduleAddr := authtypes.NewModuleAddress(bextypes.ModuleName)
	configuredModuleAddr, modulePermissions := testApp.AccountKeeper.GetModuleAddressAndPermissions(bextypes.ModuleName)
	require.Equal(t, moduleAddr, configuredModuleAddr)
	require.Empty(t, modulePermissions)
	require.True(t, testApp.BankKeeper.BlockedAddr(moduleAddr))
	admin := sdk.AccAddress(repeatedByteAddress(0x72))
	adminString, err := testApp.AccountKeeper.AddressCodec().BytesToString(admin)
	require.NoError(t, err)
	reserveString, err := testApp.AccountKeeper.AddressCodec().BytesToString(testApp.BexKeeper.GetReserveAddress(ctx, 1))
	require.NoError(t, err)
	ibcDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis(appparams.BaseDenom, "transwap", "channel-0")
	require.NoError(t, err)
	ibcDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis("gxusd", "transwap", "channel-1")
	require.NoError(t, err)
	funding := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 5))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, funding))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		minttypes.ModuleName,
		admin,
		funding,
	))
	require.NoError(t, testApp.BexKeeper.ImportGenesis(ctx, &bextypes.GenesisState{
		Admins: []string{adminString},
		Exchanges: []*bextypes.Exchange{{
			Id:                        1,
			AdminAddress:              adminString,
			ReserveAddress:            reserveString,
			DenomA:                    appparams.BaseDenom,
			PortA:                     "transwap",
			ChannelA:                  "channel-0",
			IbcDenomA:                 ibcDenomA,
			DenomB:                    "gxusd",
			PortB:                     "transwap",
			ChannelB:                  "channel-1",
			IbcDenomB:                 ibcDenomB,
			OracleSymbolAToB:          "AGXN/GXUSD",
			OracleSymbolBToA:          "GXUSD/AGXN",
			FeeBpsAToB:                25,
			FeeBpsBToA:                25,
			LimitAToB:                 "1000",
			LimitBToA:                 "1000",
			VolumeCapAToB:             "1000",
			VolumeCapBToA:             "1000",
			Status:                    bextypes.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
			Revision:                  1,
			VolumeEpochSeconds:        86_400,
			MaxOracleStalenessSeconds: 60,
		}},
		NextExchangeId: 2,
	}))
	reserveAddr := testApp.BexKeeper.GetReserveAddress(ctx, 1)
	require.NoError(t, testApp.BexKeeper.DepositReserve(ctx, adminString, 1, funding))

	// BEX's nested fee transition composes with an outer transaction cache: a
	// discarded parent rolls back the bank store, BEX ledger, and events.
	rootEventCount := len(ctx.EventManager().Events())
	outerCollectCtx, _ := ctx.CacheContext()
	require.NoError(t, testApp.BexKeeper.CollectFee(outerCollectCtx, 1, sdk.NewInt64Coin(appparams.BaseDenom, 2)))
	require.Equal(t, int64(2), testApp.BankKeeper.GetBalance(outerCollectCtx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(3), testApp.BankKeeper.GetBalance(outerCollectCtx, reserveAddr, appparams.BaseDenom).Amount.Int64())
	outerCollected, err := testApp.BexKeeper.GetCollectedFees(outerCollectCtx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), outerCollected.AmountOf(appparams.BaseDenom).Int64())
	requireEventType(t, outerCollectCtx.EventManager().Events(), bextypes.EventTypeFeesCollected)
	require.Equal(t, int64(0), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(5), testApp.BankKeeper.GetBalance(ctx, reserveAddr, appparams.BaseDenom).Amount.Int64())
	collected, err := testApp.BexKeeper.GetCollectedFees(ctx, 1)
	require.NoError(t, err)
	require.True(t, collected.IsZero())
	require.Len(t, ctx.EventManager().Events(), rootEventCount)

	require.NoError(t, testApp.BexKeeper.CollectFee(ctx, 1, sdk.NewInt64Coin(appparams.BaseDenom, 5)))
	require.NoError(t, testApp.BexKeeper.LockExchangeFee(ctx, 1, sdk.NewInt64Coin(appparams.BaseDenom, 2)))
	require.Equal(t, int64(5), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(0), testApp.BankKeeper.GetBalance(ctx, reserveAddr, appparams.BaseDenom).Amount.Int64())

	// The ledger write precedes the bank send inside a nested cache. A send
	// restriction failure must roll the ledger and events back as well.
	rootEventCount = len(ctx.EventManager().Events())
	err = testApp.BexKeeper.WithdrawFees(
		ctx,
		adminString,
		1,
		reserveAddr,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
	)
	require.ErrorIs(t, err, bextypes.ErrDirectReserveTransfer)
	collected, err = testApp.BexKeeper.GetCollectedFees(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(5), collected.AmountOf(appparams.BaseDenom).Int64())
	require.Equal(t, int64(5), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(0), testApp.BankKeeper.GetBalance(ctx, reserveAddr, appparams.BaseDenom).Amount.Int64())
	require.Len(t, ctx.EventManager().Events(), rootEventCount)

	rootEventCount = len(ctx.EventManager().Events())
	outerRefundCtx, _ := ctx.CacheContext()
	require.NoError(t, testApp.BexKeeper.RefundLockedFee(outerRefundCtx, 1, sdk.NewInt64Coin(appparams.BaseDenom, 1)))
	require.Equal(t, int64(4), testApp.BankKeeper.GetBalance(outerRefundCtx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(1), testApp.BankKeeper.GetBalance(outerRefundCtx, reserveAddr, appparams.BaseDenom).Amount.Int64())
	outerCollected, err = testApp.BexKeeper.GetCollectedFees(outerRefundCtx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(4), outerCollected.AmountOf(appparams.BaseDenom).Int64())
	outerLocked, err := testApp.BexKeeper.GetLockedFees(outerRefundCtx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), outerLocked.AmountOf(appparams.BaseDenom).Int64())
	requireEventType(t, outerRefundCtx.EventManager().Events(), bextypes.EventTypeFeesRefunded)
	require.Equal(t, int64(5), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(0), testApp.BankKeeper.GetBalance(ctx, reserveAddr, appparams.BaseDenom).Amount.Int64())
	collected, err = testApp.BexKeeper.GetCollectedFees(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(5), collected.AmountOf(appparams.BaseDenom).Int64())
	locked, err := testApp.BexKeeper.GetLockedFees(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), locked.AmountOf(appparams.BaseDenom).Int64())
	require.Len(t, ctx.EventManager().Events(), rootEventCount)

	require.NoError(t, testApp.BexKeeper.RefundLockedFee(ctx, 1, sdk.NewInt64Coin(appparams.BaseDenom, 1)))
	require.Equal(t, int64(4), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(1), testApp.BankKeeper.GetBalance(ctx, reserveAddr, appparams.BaseDenom).Amount.Int64())

	recipient := sdk.AccAddress(repeatedByteAddress(0x73))
	err = testApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		bextypes.ModuleName,
		recipient,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
	)
	require.ErrorIs(t, err, bextypes.ErrInvariantViolation)
	require.Equal(t, int64(4), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	one := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1))
	err = testApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		bextypes.ModuleName,
		minttypes.ModuleName,
		one,
	)
	require.ErrorIs(t, err, bextypes.ErrInvariantViolation)
	require.Equal(t, int64(4), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.NoError(t, testApp.BexKeeper.WithdrawFees(
		ctx,
		adminString,
		1,
		recipient,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2)),
	))
	require.Equal(t, int64(2), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, int64(2), testApp.BankKeeper.GetBalance(ctx, recipient, appparams.BaseDenom).Amount.Int64())

	err = testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(moduleAddr), uint256.NewInt(1))
	require.ErrorIs(t, err, bextypes.ErrInvariantViolation)
	require.Equal(t, int64(2), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	err = testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(moduleAddr), uint256.NewInt(3))
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Equal(t, int64(2), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())

	// Trusted module transfers may create custody surplus, but it must never
	// become attributable to an exchange or withdrawable through its fee ledger.
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, one))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		minttypes.ModuleName,
		bextypes.ModuleName,
		one,
	))
	require.Equal(t, int64(3), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	err = testApp.BexKeeper.WithdrawFees(
		ctx,
		adminString,
		1,
		recipient,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2)),
	)
	require.ErrorIs(t, err, bextypes.ErrInsufficientAvailableFees, "surplus must not become withdrawable")
	require.Equal(t, int64(3), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	err = testApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(reserveAddr), uint256.NewInt(0))
	require.ErrorIs(t, err, bextypes.ErrDirectReserveTransfer)
	require.Equal(t, int64(1), testApp.BankKeeper.GetBalance(ctx, reserveAddr, appparams.BaseDenom).Amount.Int64())
	require.NoError(t, testApp.BexKeeper.AssertFeeSolvency(ctx))

	require.NotContains(t, ModuleOrderEndBlockers(), bextypes.ModuleName)
	_, hasEndBlocker := testApp.ModuleManager.Modules[bextypes.ModuleName].(appmodule.HasEndBlocker)
	require.False(t, hasEndBlocker)
	initOrder := ModuleOrderInitGenesis()
	bankIndex := indexOf(initOrder, banktypes.ModuleName)
	bexIndex := indexOf(initOrder, bextypes.ModuleName)
	require.NotEqual(t, -1, bankIndex)
	require.NotEqual(t, -1, bexIndex)
	require.Less(t, bankIndex, bexIndex)
}

func TestBexInitGenesisAuditsActualBankBacking(t *testing.T) {
	for _, tc := range []struct {
		name        string
		backing     int64
		wantErr     bool
		wantSurplus int64
	}{
		{name: "underbacked", backing: 0, wantErr: true},
		{name: "exactly backed", backing: 1},
		{name: "surplus backed", backing: 2, wantSurplus: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testApp := NewApp(
				log.NewNopLogger(),
				dbm.NewMemDB(),
				true,
				simtestutil.EmptyAppOptions{},
				baseapp.SetChainID(appparams.SDKChainID),
			)
			t.Cleanup(func() { require.NoError(t, testApp.Close()) })
			ctx := testApp.NewNextBlockContext(cmtproto.Header{
				ChainID: appparams.SDKChainID,
				Height:  1,
				Time:    time.Unix(1_700_000_000, 0),
			})

			admin := sdk.AccAddress(repeatedByteAddress(0x74))
			adminString, err := testApp.AccountKeeper.AddressCodec().BytesToString(admin)
			require.NoError(t, err)
			reserveString, err := testApp.BexKeeper.GetReserveAddressString(ctx, 1)
			require.NoError(t, err)
			ibcDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis("agxn", "transwap", "channel-0")
			require.NoError(t, err)
			ibcDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis("gxusd", "transwap", "channel-1")
			require.NoError(t, err)
			genesis := &bextypes.GenesisState{
				Admins: []string{adminString},
				Exchanges: []*bextypes.Exchange{{
					Id:                        1,
					AdminAddress:              adminString,
					ReserveAddress:            reserveString,
					DenomA:                    "agxn",
					PortA:                     "transwap",
					ChannelA:                  "channel-0",
					IbcDenomA:                 ibcDenomA,
					DenomB:                    "gxusd",
					PortB:                     "transwap",
					ChannelB:                  "channel-1",
					IbcDenomB:                 ibcDenomB,
					OracleSymbolAToB:          "AGXN/GXUSD",
					OracleSymbolBToA:          "GXUSD/AGXN",
					FeeBpsAToB:                25,
					FeeBpsBToA:                25,
					LimitAToB:                 "1000",
					LimitBToA:                 "1000",
					VolumeCapAToB:             "1000",
					VolumeCapBToA:             "1000",
					Revision:                  1,
					VolumeWindowGeneration:    1,
					Status:                    bextypes.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
					VolumeEpochSeconds:        86_400,
					MaxOracleStalenessSeconds: 60,
				}},
				CollectedFees: []*bextypes.FeeGenesis{{
					ExchangeId: 1,
					Coins:      sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
				}},
				NextExchangeId: 2,
			}

			if tc.backing > 0 {
				backing := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, tc.backing))
				require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, backing))
				require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToModule(
					ctx,
					minttypes.ModuleName,
					bextypes.ModuleName,
					backing,
				))
			}

			cacheCtx, _ := ctx.CacheContext()
			err = bexmodule.NewAppModule(testApp.BexKeeper).InitGenesis(cacheCtx, bexGenesisSource(t, genesis))
			if tc.wantErr {
				require.ErrorIs(t, err, bextypes.ErrInvariantViolation)
				_, getErr := testApp.BexKeeper.GetExchange(ctx, 1)
				require.ErrorIs(t, getErr, bextypes.ErrExchangeNotFound)
				return
			}
			require.NoError(t, err)
			require.NoError(t, testApp.BexKeeper.AssertFeeSolvency(cacheCtx))
			moduleBalance := testApp.BankKeeper.GetBalance(cacheCtx, authtypes.NewModuleAddress(bextypes.ModuleName), appparams.BaseDenom).Amount
			require.Equal(t, int64(1)+tc.wantSurplus, moduleBalance.Int64())
		})
	}
}

func bexGenesisSource(t *testing.T, genesis *bextypes.GenesisState) appmodule.GenesisSource {
	t.Helper()
	fields := map[string]any{
		"admins":             genesis.GetAdmins(),
		"exchanges":          genesis.GetExchanges(),
		"collected_fees":     genesis.GetCollectedFees(),
		"locked_fees":        genesis.GetLockedFees(),
		"volume_windows":     genesis.GetVolumeWindows(),
		"reserve_depositors": genesis.GetReserveDepositors(),
		"next_exchange_id":   genesis.GetNextExchangeId(),
	}
	encoded := make(map[string][]byte, len(fields))
	for field, value := range fields {
		bz, err := json.Marshal(value)
		require.NoError(t, err)
		encoded[field] = bz
	}
	return func(field string) (io.ReadCloser, error) {
		bz, ok := encoded[field]
		if !ok {
			return nil, nil
		}
		return io.NopCloser(bytes.NewReader(bz)), nil
	}
}

type gogoRuntimeMessage interface {
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

func executeBexRuntimeMsg(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request sdk.Msg,
	response gogoRuntimeMessage,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "BEX Msg RPC executed more than once")
	handler := testApp.MsgServiceRouter().Handler(request)
	require.NotNil(t, handler, "BEX Msg RPC %s is not registered", method)
	result, err := handler(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.MsgResponses, 1)
	require.Equal(t, "/guru.bex.v1.Msg"+method+"Response", result.MsgResponses[0].TypeUrl)
	require.NoError(t, response.Unmarshal(result.Data))
	executed[method] = struct{}{}
	t.Logf("runtime Msg/%s => %v", method, response)
}

func executeBexRuntimeQuery(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request gogoRuntimeMessage,
	response gogoRuntimeMessage,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "BEX Query RPC executed more than once")
	handler := testApp.GRPCQueryRouter().Route("/guru.bex.v1.Query/" + method)
	require.NotNil(t, handler, "BEX Query RPC %s is not registered", method)
	requestBytes, err := request.Marshal()
	require.NoError(t, err)
	queryResult, err := handler(ctx, &abci.RequestQuery{Data: requestBytes, Height: ctx.BlockHeight()})
	require.NoError(t, err)
	require.NotNil(t, queryResult)
	require.Equal(t, ctx.BlockHeight(), queryResult.Height)
	require.NoError(t, response.Unmarshal(queryResult.Value))
	executed[method] = struct{}{}
	t.Logf("runtime Query/%s => %v", method, response)
}

func requireBexLedgerCoin(t *testing.T, ledger *bextypes.FeeLedger, denom, amount string) {
	t.Helper()
	require.NotNil(t, ledger)
	require.Len(t, ledger.GetCoins(), 1)
	require.Equal(t, denom, ledger.GetCoins()[0].GetDenom())
	require.Equal(t, amount, ledger.GetCoins()[0].Amount.String())
}

func requireBexServiceMethodsExecuted(t *testing.T, service reflect.Type, executed map[string]struct{}) {
	t.Helper()
	require.Len(t, executed, service.NumMethod())
	for i := 0; i < service.NumMethod(); i++ {
		require.Contains(t, executed, service.Method(i).Name, "runtime RPC coverage must track the generated service interface")
	}
}

func requireEventType(t *testing.T, events sdk.Events, eventType string) {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return
		}
	}
	require.Failf(t, "missing event", "event type %q was not emitted", eventType)
}

func repeatedByteAddress(value byte) []byte {
	address := make([]byte, 20)
	for i := range address {
		address[i] = value
	}
	return address
}
