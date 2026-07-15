package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
)

func TestExportGenesisRejectsCorruptFeeState(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(t *testing.T, f keeperTestFixture, exchange *bexv1.Exchange)
	}{
		{
			name: "fee custody is under-backed",
			corrupt: func(t *testing.T, f keeperTestFixture, exchange *bexv1.Exchange) {
				fee := sdk.NewInt64Coin(exchange.GetDenomA(), 10)
				collectFee(t, f, exchange.GetId(), fee)
				f.bankKeeper.SetBalance(
					authtypes.NewModuleAddress(types.ModuleName),
					sdk.NewCoins(sdk.NewInt64Coin(fee.Denom, fee.Amount.Int64()-1)),
				)
			},
		},
		{
			name: "locked fee exceeds collected fee",
			corrupt: func(t *testing.T, f keeperTestFixture, exchange *bexv1.Exchange) {
				fee := sdk.NewInt64Coin(exchange.GetDenomA(), 10)
				collectFee(t, f, exchange.GetId(), fee)
				require.NoError(t, f.keeper.lockedFees.Set(
					f.ctx,
					exchange.GetId(),
					coinsToLedger(sdk.NewCoins(sdk.NewInt64Coin(fee.Denom, fee.Amount.Int64()+1))),
				))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			require.NoError(t, f.keeper.RegisterAdmin(f.ctx, f.moderator, f.admin))
			exchange := registerExchange(t, f, bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE)
			tc.corrupt(t, f, exchange)

			genesis, err := f.keeper.ExportGenesis(f.ctx)
			require.ErrorIs(t, err, types.ErrInvariantViolation)
			require.Nil(t, genesis)
		})
	}
}
