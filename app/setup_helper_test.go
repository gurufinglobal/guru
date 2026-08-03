package app

import (
	"testing"

	"cosmossdk.io/log/v2"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/baseapp/txnrunner"
	"github.com/stretchr/testify/require"
)

func TestWrapGuruTxRunnerExposesBlockSTMToBlockGasGuard(t *testing.T) {
	bApp := baseapp.NewBaseApp("block-stm-guard", log.NewTestLogger(t), dbm.NewMemDB(), nil)
	bApp.SetDisableBlockGasMeter(true)
	bApp.SetBlockSTMTxRunner(wrapGuruTxRunner(
		txnrunner.NewSTMRunner(nil, nil, 1, true, nil),
	))

	require.Panics(t, func() {
		bApp.SetDisableBlockGasMeter(false)
	})
}

func TestWrapGuruTxRunnerAllowsSequentialBlockGasMeter(t *testing.T) {
	bApp := baseapp.NewBaseApp("sequential-guard", log.NewTestLogger(t), dbm.NewMemDB(), nil)
	bApp.SetDisableBlockGasMeter(true)
	bApp.SetBlockSTMTxRunner(wrapGuruTxRunner(txnrunner.NewDefaultRunner(nil)))

	require.NotPanics(t, func() {
		bApp.SetDisableBlockGasMeter(false)
	})
}
