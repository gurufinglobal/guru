package cli

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestGetTxCmdIncludesOracleCommands(t *testing.T) {
	cmd := GetTxCmd()

	for _, name := range []string{"update-params", "upsert-task", "remove-task"} {
		_, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
	}
}

func TestGetQueryCmdIncludesOracleCommands(t *testing.T) {
	cmd := GetQueryCmd()

	for _, name := range []string{"params", "active-tasks", "task", "latest-value", "latest-values", "history"} {
		_, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
	}
}

func TestNewMsgUpdateParams(t *testing.T) {
	msg, err := newMsgUpdateParams("guru1moderator", []string{"3", "4", "100"})

	require.NoError(t, err)
	require.Equal(t, "guru1moderator", msg.GetModerator())
	require.Equal(t, uint32(3), msg.GetParams().GetMinValidators())
	require.Equal(t, uint32(4), msg.GetParams().GetMinSources())
	require.Equal(t, uint32(100), msg.GetParams().GetHistoryLimit())
}

func TestNewMsgUpdateParamsRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"0", "4", "100"},
		{"3", "0", "100"},
		{"3", "4", "0"},
		{"-1", "4", "100"},
		{"abc", "4", "100"},
	} {
		_, err := newMsgUpdateParams("guru1moderator", args)
		require.Error(t, err)
	}
}

func TestNewMsgUpsertTaskUsesNumericValueType(t *testing.T) {
	msg, err := newMsgUpsertTask("guru1moderator", "btc/usd", "5", true)

	require.NoError(t, err)
	require.Equal(t, "guru1moderator", msg.GetModerator())
	require.Equal(t, "btc/usd", msg.GetTask().GetSymbol())
	require.Equal(t, oraclev1.ValueType_VALUE_TYPE_NUMERIC, msg.GetTask().GetValueType())
	require.True(t, msg.GetTask().GetEnabled())
	require.Equal(t, uint32(5), msg.GetTask().GetSubmissionInterval())
}

func TestNewMsgUpsertTaskAllowsDisabledNumericTask(t *testing.T) {
	msg, err := newMsgUpsertTask("guru1moderator", "btc/usd", "5", false)

	require.NoError(t, err)
	require.False(t, msg.GetTask().GetEnabled())
	require.Equal(t, oraclev1.ValueType_VALUE_TYPE_NUMERIC, msg.GetTask().GetValueType())
}

func TestNewMsgUpsertTaskRejectsInvalidInterval(t *testing.T) {
	for _, raw := range []string{"0", "-1", "abc"} {
		_, err := newMsgUpsertTask("guru1moderator", "btc/usd", raw, true)
		require.Error(t, err)
	}
}

func TestUpsertTaskCommandHasNoValueTypeFlag(t *testing.T) {
	cmd := CmdUpsertTask()

	require.NotNil(t, cmd.Flags().Lookup(flagEnabled))
	require.Nil(t, cmd.Flags().Lookup("value-type"))
}

func TestReadPageRequest(t *testing.T) {
	cmd := CmdQueryActiveTasks()
	require.NoError(t, cmd.Flags().Set(flags.FlagLimit, "25"))
	require.NoError(t, cmd.Flags().Set(flags.FlagOffset, "50"))
	require.NoError(t, cmd.Flags().Set(flags.FlagCountTotal, "true"))
	require.NoError(t, cmd.Flags().Set(flags.FlagReverse, "true"))

	pageReq, err := readPageRequest(cmd)

	require.NoError(t, err)
	require.Equal(t, uint64(25), pageReq.GetLimit())
	require.Equal(t, uint64(50), pageReq.GetOffset())
	require.True(t, pageReq.GetCountTotal())
	require.True(t, pageReq.GetReverse())
}
