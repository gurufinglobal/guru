package v2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestV2ExchangeSourceTimeoutTimestamp(t *testing.T) {
	blockTime := time.Unix(1_700_000_000, 0)
	ctx := sdk.Context{}.WithBlockTime(blockTime)

	expected := uint64(blockTime.Add(10 * time.Minute).UnixNano())
	require.Equal(t, expected, v2ExchangeSourceTimeoutTimestamp(ctx))
}
