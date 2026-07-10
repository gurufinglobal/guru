package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetQueryCmdHasSubcommands(t *testing.T) {
	cmd := GetQueryCmd()
	require.Equal(t, "ibc-transwap", cmd.Use)
	require.Len(t, cmd.Commands(), 5)
}

func TestReadPulsarPageRequest(t *testing.T) {
	cmd := GetCmdQueryDenoms()
	pageReq, err := readPulsarPageRequest(cmd)
	require.NoError(t, err)
	require.NotNil(t, pageReq)
	require.Equal(t, uint64(100), pageReq.Limit)
	require.Equal(t, []byte{}, pageReq.Key)

	require.NoError(t, cmd.Flags().Set("limit", "25"))
	pageReq, err = readPulsarPageRequest(cmd)
	require.NoError(t, err)
	require.Equal(t, uint64(25), pageReq.Limit)
}
