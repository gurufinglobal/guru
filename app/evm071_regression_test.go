package app

import (
	"testing"
	"time"

	"cosmossdk.io/log"
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
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

func TestEVM061LockedBalanceSnapshotThroughGuruApp(t *testing.T) {
	testApp, ctx := newEVM061RegressionApp(t)

	addr := sdk.AccAddress(repeatedByteAddress(0x91))
	baseAccount, ok := testApp.AccountKeeper.NewAccountWithAddress(ctx, addr).(*authtypes.BaseAccount)
	require.True(t, ok)
	testApp.AccountKeeper.SetAccount(ctx, baseAccount)

	total := sdk.NewInt64Coin(appparams.BaseDenom, 100)
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(total)))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		minttypes.ModuleName,
		addr,
		sdk.NewCoins(total),
	))

	start := ctx.BlockTime().Unix()
	vestingAccount, err := vestingtypes.NewContinuousVestingAccount(
		baseAccount,
		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 40)),
		start,
		start+3_600,
	)
	require.NoError(t, err)
	testApp.AccountKeeper.SetAccount(ctx, vestingAccount)

	ethAddr := common.BytesToAddress(addr)
	account := testApp.EVMKeeper.GetAccount(ctx, ethAddr)
	require.NotNil(t, account)
	require.Equal(t, "60", account.Balance.String())
	lockedBalance := account.LockedBalanceSnapshot()
	require.NotNil(t, lockedBalance)
	require.Equal(t, "40", lockedBalance.String())

	// Simulate the staking precompile's bank and vesting-account transition:
	// 20 locked coins leave the bank balance and become DelegatedVesting, while
	// the EVM balance handler has already deducted the same 20 from its view.
	// SetAccount must reconstruct the final bank balance with the locked value
	// captured before the precompile, rather than re-reading the now lower lock.
	vestingAccount.DelegatedVesting = sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 20))
	testApp.AccountKeeper.SetAccount(ctx, vestingAccount)
	require.Equal(t, int64(20), testApp.BankKeeper.LockedCoins(ctx, addr).AmountOf(appparams.BaseDenom).Int64())

	account.Balance = uint256.NewInt(40)
	require.NoError(t, testApp.EVMKeeper.SetAccount(ctx, ethAddr, *account))

	require.Equal(t, int64(80), testApp.BankKeeper.GetBalance(ctx, addr, appparams.BaseDenom).Amount.Int64())
	require.Equal(t, uint64(0), testApp.AccountKeeper.GetAccount(ctx, addr).GetSequence())
	require.Equal(t, "80", testApp.EVMKeeper.GetBalance(ctx, ethAddr).String())
}

func TestEVM061ProtectedBalanceWritesThroughGuruApp(t *testing.T) {
	testApp, ctx := newEVM061RegressionApp(t)

	moduleAddr := authtypes.NewModuleAddress(stakingtypes.BondedPoolName)
	require.NotNil(t, testApp.AccountKeeper.GetModuleAccount(ctx, stakingtypes.BondedPoolName))

	initialModuleBalance := sdk.NewInt64Coin(appparams.BaseDenom, 10)
	require.NoError(t, testApp.BankKeeper.MintCoins(
		ctx,
		minttypes.ModuleName,
		sdk.NewCoins(initialModuleBalance),
	))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		minttypes.ModuleName,
		stakingtypes.BondedPoolName,
		sdk.NewCoins(initialModuleBalance),
	))

	moduleEthAddr := common.BytesToAddress(moduleAddr)
	for _, amount := range []uint64{9, 10, 11} {
		err := testApp.EVMKeeper.SetBalance(ctx, moduleEthAddr, uint256.NewInt(amount))
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		require.Equal(t, int64(10), testApp.BankKeeper.GetBalance(ctx, moduleAddr, appparams.BaseDenom).Amount.Int64())
	}

	// Guru also blocks static precompile addresses even though they are not
	// module accounts. This exercises the independent blocked-address branch.
	precompileAddr := sdk.AccAddress(common.HexToAddress(evmtypes.BankPrecompileAddress).Bytes())
	require.Nil(t, testApp.AccountKeeper.GetAccount(ctx, precompileAddr))
	require.True(t, testApp.BankKeeper.BlockedAddr(precompileAddr))

	supplyBefore := testApp.BankKeeper.GetSupply(ctx, appparams.BaseDenom)
	cacheCtx, _ := ctx.CacheContext()
	err := testApp.EVMKeeper.SetBalance(
		cacheCtx,
		common.BytesToAddress(precompileAddr),
		uint256.NewInt(1),
	)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.True(t, testApp.BankKeeper.GetBalance(ctx, precompileAddr, appparams.BaseDenom).IsZero())
	require.Equal(t, supplyBefore, testApp.BankKeeper.GetSupply(ctx, appparams.BaseDenom))
}

func newEVM061RegressionApp(t *testing.T) (*App, sdk.Context) {
	t.Helper()

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
	return testApp, ctx
}
