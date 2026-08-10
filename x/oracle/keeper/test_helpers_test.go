package keeper

import (
	"bytes"
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

type keeperTestFixture struct {
	ctx       sdk.Context
	keeper    Keeper
	moderator string
}

func setupKeeperFixture(t *testing.T) keeperTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_oracle_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	moderator, err := accountCodec.BytesToString(bytes.Repeat([]byte{0x01}, 20))
	require.NoError(t, err)

	keeper := NewKeeper(
		runtime.NewKVStoreService(key),
		codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
		accountCodec,
		mockConstitutionKeeper{moderator: moderator},
	)
	require.NoError(t, keeper.SetParams(testCtx.Ctx, DefaultParams()))

	return keeperTestFixture{
		ctx:       testCtx.Ctx,
		keeper:    keeper,
		moderator: moderator,
	}
}

type mockConstitutionKeeper struct {
	moderator string
}

func (m mockConstitutionKeeper) GetModeratorAddress(context.Context) (string, error) {
	return m.moderator, nil
}
