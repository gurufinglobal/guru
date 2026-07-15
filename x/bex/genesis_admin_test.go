package bex

import (
	"testing"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestGenesisAllowsLiveExchangeAdminOutsideBEXRegistry(t *testing.T) {
	am, ctx := setupAppModule(t)
	genesis := validGenesisState(t, am, ctx)
	bexAdmin := genesis.GetAdmins()[0]
	exchangeAdmin := rootAddressString(t, 0x13)
	genesis.Exchanges[0].AdminAddress = exchangeAdmin

	require.NotEqual(t, bexAdmin, exchangeAdmin)
	require.Equal(t, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE, genesis.Exchanges[0].GetStatus())
	require.NoError(t, am.validateGenesisState(ctx, genesis))

	genesis.Admins = nil
	require.NoError(t, am.validateGenesisState(ctx, genesis))
}

func TestGenesisRejectsNonCanonicalDecimalConfig(t *testing.T) {
	am, ctx := setupAppModule(t)

	for _, value := range []string{"010", "+10", " 10 "} {
		genesis := validGenesisState(t, am, ctx)
		genesis.Exchanges[0].LimitAToB = value
		require.ErrorIs(t, am.validateGenesisState(ctx, genesis), types.ErrInvalidGenesis)
	}
}

func TestGenesisRejectsNonCanonicalVolumeWindowDecimal(t *testing.T) {
	am, ctx := setupAppModule(t)
	genesis := validGenesisState(t, am, ctx)
	genesis.VolumeWindows[0].Amount = " 5 "
	require.ErrorIs(t, am.validateGenesisState(ctx, genesis), types.ErrInvalidGenesis)
}
