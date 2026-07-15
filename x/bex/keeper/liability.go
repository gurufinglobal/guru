package keeper

import (
	"context"
	"errors"
	"sort"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

// GetRefundAccountingExchangeIDs returns the deterministic union of exchange
// IDs that own pending-liability or locked-fee ledgers. Cross-module audits use
// the union so an orphan BEX ledger cannot be hidden by deleting its TransSwap
// refund records.
func (k Keeper) GetRefundAccountingExchangeIDs(ctx context.Context) ([]uint64, error) {
	ids := make(map[uint64]struct{})
	if err := k.pendingLiabilities.Walk(ctx, nil, func(exchangeID uint64, _ *bexv1.FeeLedger) (bool, error) {
		ids[exchangeID] = struct{}{}
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.lockedFees.Walk(ctx, nil, func(exchangeID uint64, _ *bexv1.FeeLedger) (bool, error) {
		ids[exchangeID] = struct{}{}
		return false, nil
	}); err != nil {
		return nil, err
	}

	ordered := make([]uint64, 0, len(ids))
	for exchangeID := range ids {
		ordered = append(ordered, exchangeID)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered, nil
}

// GetPendingLiabilities returns the aggregate gross refund obligation that
// remains unresolved until output/refund acknowledgement success or claim.
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
	err := executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		exchange, err := k.GetActiveExchange(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
			return types.ErrInvalidRoute.Wrap("adding pending liability requires an active exchange")
		}
		if err := validateExchangeReserveDenom(exchange, liability.Denom); err != nil {
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
		if err := k.pendingLiabilities.Set(cacheCtx, exchangeID, coinsToLedger(pending)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (k Keeper) ReleasePendingLiability(ctx context.Context, exchangeID uint64, liability sdk.Coin) error {
	if err := validateFeeCoin(liability); err != nil {
		return err
	}
	err := executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		exchange, err := k.GetActiveExchange(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if err := validateExchangeReserveDenom(exchange, liability.Denom); err != nil {
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
		pending = pending.Sub(liability)
		return k.pendingLiabilities.Set(cacheCtx, exchangeID, coinsToLedger(pending))
	})
	if err != nil {
		return err
	}
	return nil
}
