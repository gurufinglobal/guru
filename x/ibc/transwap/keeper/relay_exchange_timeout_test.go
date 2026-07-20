package keeper

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestValidateInheritedTimeout(t *testing.T) {
	blockTime := time.Unix(1_700_000_000, 123)
	ctx := sdk.Context{}.WithBlockTime(blockTime)
	current := uint64(blockTime.UnixNano()) //nolint:gosec // G115: block time cannot be negative

	minimum := current + uint64(minimumForwardTimeout)
	require.Error(t, validateInheritedTimeout(ctx, minimum))
	require.NoError(t, validateInheritedTimeout(ctx, minimum+1))
	require.Error(t, validateInheritedTimeout(ctx, current))

	maximum := current + uint64(maximumForwardTimeout)
	require.NoError(t, validateInheritedTimeout(ctx, maximum))
	require.ErrorContains(t, validateInheritedTimeout(ctx, maximum+1), "too far in the future")
	require.ErrorContains(t, validateInheritedTimeout(ctx, math.MaxUint64), "too far in the future")
}
