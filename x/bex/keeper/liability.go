package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

// GetPendingLiabilities returns the aggregate net input principal that must
// remain unavailable while cross-chain output or refund packets are in flight.
func (k Keeper) GetPendingLiabilities(ctx context.Context, exchangeID uint64) (sdk.Coins, error) {
	ledger, err := k.pendingLiabilities.Get(ctx, exchangeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return sdk.Coins{}, nil
		}
		return nil, err
	}
	return ledgerToCoins(ledger)
}

func (k Keeper) AddPendingLiability(ctx context.Context, exchangeID uint64, liability sdk.Coin) error {
	if err := validateFeeCoin(liability); err != nil {
		return err
	}
	return executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		exchange, err := k.GetActiveExchange(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
			return types.ErrInvalidRoute.Wrap("adding pending liability requires an active exchange")
		}
		if err := validateExchangeFeeDenom(exchange, liability.Denom); err != nil {
			return err
		}
		pending, err := k.GetPendingLiabilities(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		pending, err = checkedAddFeeCoin(pending, liability)
		if err != nil {
			return err
		}
		return k.pendingLiabilities.Set(cacheCtx, exchangeID, coinsToLedger(pending))
	})
}

func (k Keeper) ReleasePendingLiability(ctx context.Context, exchangeID uint64, liability sdk.Coin) error {
	if err := validateFeeCoin(liability); err != nil {
		return err
	}
	return executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		exchange, err := k.GetActiveExchange(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if err := validateExchangeFeeDenom(exchange, liability.Denom); err != nil {
			return err
		}
		pending, err := k.GetPendingLiabilities(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		coins := sdk.NewCoins(liability)
		if !hasCoins(pending, coins) {
			return types.ErrInvariantViolation.Wrap("release exceeds pending reserve liability")
		}
		return k.pendingLiabilities.Set(cacheCtx, exchangeID, coinsToLedger(pending.Sub(liability)))
	})
}
