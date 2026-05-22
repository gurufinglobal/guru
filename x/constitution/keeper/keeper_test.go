package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewKeeperInitializesCollections(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	run := func() {
		_, _ = f.keeper.params.Has(f.ctx)
	}
	require.NotPanics(t, run)
}

func TestKeeperLogger(t *testing.T) {
	f := setupKeeperFixtureWithoutParams(t)

	logger := f.keeper.Logger(f.ctx)
	require.NotNil(t, logger)
	require.NotPanics(t, func() { logger.Info("keeper logger test") })
}
