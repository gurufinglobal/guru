package app

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"
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
	"github.com/ethereum/go-ethereum/common"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bexmodule "github.com/gurufinglobal/guru/v3/x/bex"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
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
	moderator := wiringAddress(t, 0x22)
	require.NoError(t, testApp.ConstitutionKeeper.SetModeratorAddress(ctx, moderator))

	admin := sdk.AccAddress(repeatedByteAddress(0x61))
	adminString, err := testApp.AccountKeeper.AddressCodec().BytesToString(admin)
	require.NoError(t, err)
	require.NoError(t, testApp.BexKeeper.RegisterAdmin(ctx, moderator, adminString))

	exchange, err := testApp.BexKeeper.RegisterExchange(ctx, &bexv1.MsgRegisterExchange{
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
		Status:                    bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE,
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

	exchange, err := testApp.BexKeeper.RegisterExchange(ctx, &bexv1.MsgRegisterExchange{
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
		Status:                    bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE,
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
	ibcDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis(appparams.BaseDenom, "transfer", "channel-0")
	require.NoError(t, err)
	ibcDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis("gxusd", "transfer", "channel-1")
	require.NoError(t, err)
	funding := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 5))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, funding))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		minttypes.ModuleName,
		admin,
		funding,
	))
	require.NoError(t, testApp.BexKeeper.ImportGenesis(ctx, &bexv1.GenesisState{
		Admins: []string{adminString},
		Exchanges: []*bexv1.Exchange{{
			Id:                        1,
			AdminAddress:              adminString,
			ReserveAddress:            reserveString,
			DenomA:                    appparams.BaseDenom,
			PortA:                     "transfer",
			ChannelA:                  "channel-0",
			IbcDenomA:                 ibcDenomA,
			DenomB:                    "gxusd",
			PortB:                     "transfer",
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
			Status:                    bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
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
			ibcDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis("agxn", "transfer", "channel-0")
			require.NoError(t, err)
			ibcDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis("gxusd", "transfer", "channel-1")
			require.NoError(t, err)
			genesis := &bexv1.GenesisState{
				Admins: []string{adminString},
				Exchanges: []*bexv1.Exchange{{
					Id:                        1,
					AdminAddress:              adminString,
					ReserveAddress:            reserveString,
					DenomA:                    "agxn",
					PortA:                     "transfer",
					ChannelA:                  "channel-0",
					IbcDenomA:                 ibcDenomA,
					DenomB:                    "gxusd",
					PortB:                     "transfer",
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
					Status:                    bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
					VolumeEpochSeconds:        86_400,
					MaxOracleStalenessSeconds: 60,
				}},
				CollectedFees: []*bexv1.FeeGenesis{{
					ExchangeId: 1,
					Coins: []*basev1beta1.Coin{{
						Denom:  appparams.BaseDenom,
						Amount: "1",
					}},
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

func bexGenesisSource(t *testing.T, genesis *bexv1.GenesisState) appmodule.GenesisSource {
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
