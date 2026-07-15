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
	current := uint64(blockTime.UnixNano()) //nolint:gosec // G115: block time cannot be negative

	minimum := current + uint64(minimumForwardTimeout)
	require.Error(t, validateInheritedTimeout(ctx, minimum))
	require.NoError(t, validateInheritedTimeout(ctx, minimum+1))
	require.Error(t, validateInheritedTimeout(ctx, current))
}
