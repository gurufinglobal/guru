package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestValidateInheritedTimeout(t *testing.T) {
	blockTime := time.Unix(1_700_000_000, 123)
	ctx := sdk.Context{}.WithBlockTime(blockTime)
	current := uint64(blockTime.UnixNano())

	require.NoError(t, validateInheritedTimeout(ctx, current))
	require.NoError(t, validateInheritedTimeout(ctx, current+1))
	require.Error(t, validateInheritedTimeout(ctx, current-1))
}

func TestToV2TimeoutSeconds(t *testing.T) {
	require.Equal(t, uint64(0), toV2TimeoutSeconds(0))
	require.Equal(t, uint64(17), toV2TimeoutSeconds(17*uint64(time.Second)))
	require.Equal(t, uint64(18), toV2TimeoutSeconds(17*uint64(time.Second)+1))
}
