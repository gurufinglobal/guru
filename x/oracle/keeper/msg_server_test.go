package keeper

import (
	"bytes"
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestMsgServerAllowsModeratorToUpdateParamsAndTasks(t *testing.T) {
	f := setupKeeperFixture(t)
	msgServer := NewMsgServer(&f.keeper)
	goCtx := f.ctx

	params := &oraclev1.Params{
		MinValidators: 2,
		MinSources:    4,
		HistoryLimit:  5,
	}
	_, err := msgServer.UpdateParams(goCtx, &oraclev1.MsgUpdateParams{
		Moderator: f.moderator,
		Params:    params,
	})
	require.NoError(t, err)
	storedParams, err := f.keeper.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, params.GetMinValidators(), storedParams.GetMinValidators())
	require.Equal(t, params.GetMinSources(), storedParams.GetMinSources())
	require.Equal(t, params.GetHistoryLimit(), storedParams.GetHistoryLimit())

	_, err = msgServer.UpsertTask(goCtx, &oraclev1.MsgUpsertTask{
		Moderator: f.moderator,
		Task: &oraclev1.OracleTask{
			Symbol:             " BTC/USD ",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		},
	})
	require.NoError(t, err)
	task, err := f.keeper.GetTask(f.ctx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, "BTC/USD", task.GetSymbol())

	_, err = msgServer.RemoveTask(goCtx, &oraclev1.MsgRemoveTask{
		Moderator: f.moderator,
		Symbol:    " BTC/USD ",
	})
	require.NoError(t, err)
	_, err = f.keeper.GetTask(f.ctx, "BTC/USD")
	require.Error(t, err)
}

func TestMsgServerRejectsGovAndArbitraryAuthorities(t *testing.T) {
	f := setupKeeperFixture(t)
	msgServer := NewMsgServer(&f.keeper)
	goCtx := f.ctx

	for _, authority := range []string{
		testGovAddress(t),
		testAccountAddress(t, 0x02),
	} {
		_, err := msgServer.UpdateParams(goCtx, &oraclev1.MsgUpdateParams{
			Moderator: authority,
			Params:    DefaultParams(),
		})
		require.Error(t, err)

		_, err = msgServer.UpsertTask(goCtx, &oraclev1.MsgUpsertTask{
			Moderator: authority,
			Task: &oraclev1.OracleTask{
				Symbol:             "BTC/USD",
				ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 1,
			},
		})
		require.Error(t, err)
	}
}

func testGovAddress(t *testing.T) string {
	t.Helper()

	codec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	address, err := codec.BytesToString(authtypes.NewModuleAddress(govtypes.ModuleName))
	require.NoError(t, err)
	return address
}

func testAccountAddress(t *testing.T, b byte) string {
	t.Helper()

	codec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	address, err := codec.BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}
