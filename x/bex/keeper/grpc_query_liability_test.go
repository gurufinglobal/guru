package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestQueryPendingLiabilities(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
	exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
	queryServer := NewQueryServer(&f.keeper)

	emptyResponse, err := queryServer.PendingLiabilities(f.ctx, &bexv1.QueryPendingLiabilitiesRequest{
		ExchangeId: exchange.GetId(),
	})
	require.NoError(t, err)
	emptyCoins, err := ledgerToCoins(emptyResponse.GetLedger())
	require.NoError(t, err)
	require.True(t, emptyCoins.IsZero())

	liability := sdk.NewInt64Coin(exchange.GetIbcDenomA(), 40)
	require.NoError(t, f.keeper.AddPendingLiability(f.ctx, exchange.GetId(), liability))

	response, err := queryServer.PendingLiabilities(f.ctx, &bexv1.QueryPendingLiabilitiesRequest{
		ExchangeId: exchange.GetId(),
	})
	require.NoError(t, err)
	coins, err := ledgerToCoins(response.GetLedger())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(liability), coins)
}

func TestQueryPendingLiabilitiesRejectsInvalidRequests(t *testing.T) {
	f := setupKeeperFixture(t)
	queryServer := NewQueryServer(&f.keeper)

	_, err := queryServer.PendingLiabilities(f.ctx, nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)

	_, err = queryServer.PendingLiabilities(f.ctx, &bexv1.QueryPendingLiabilitiesRequest{ExchangeId: 404})
	require.ErrorIs(t, err, types.ErrExchangeNotFound)
}
