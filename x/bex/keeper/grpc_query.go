package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

var _ types.QueryServer = QueryServer{}

type QueryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

func NewQueryServer(keeper *Keeper) QueryServer {
	return QueryServer{keeper: keeper}
}

func (q QueryServer) Exchange(ctx context.Context, req *types.QueryExchangeRequest) (*types.QueryExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchange, err := q.keeper.GetExchange(ctx, req.GetExchangeId())
	if err != nil {
		return nil, err
	}
	return &types.QueryExchangeResponse{Exchange: exchange}, nil
}

func (q QueryServer) Exchanges(ctx context.Context, req *types.QueryExchangesRequest) (*types.QueryExchangesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchanges, page, err := sdkquery.CollectionFilteredPaginate(
		ctx,
		q.keeper.exchanges,
		req.GetPagination(),
		func(_ uint64, exchange *types.Exchange) (bool, error) {
			return req.GetIncludeDeleted() || exchange.GetStatus() != types.ExchangeStatus_EXCHANGE_STATUS_DELETED, nil
		},
		func(_ uint64, exchange *types.Exchange) (*types.Exchange, error) {
			return exchange, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return &types.QueryExchangesResponse{Exchanges: exchanges, Pagination: page}, nil
}

func (q QueryServer) ExchangesByExchangeAdmin(ctx context.Context, req *types.QueryExchangesByExchangeAdminRequest) (*types.QueryExchangesByExchangeAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchangeAdmin, _, err := q.keeper.canonicalAddress(req.GetExchangeAdminAddress())
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrapf("invalid exchange admin address: %v", err)
	}
	exchanges, page, err := sdkquery.CollectionPaginate(
		ctx,
		q.keeper.exchangesByAdmin,
		req.GetPagination(),
		func(key collections.Pair[string, uint64], _ collections.NoValue) (*types.Exchange, error) {
			exchange, err := q.keeper.GetExchange(ctx, key.K2())
			if err != nil {
				if errors.Is(err, types.ErrExchangeNotFound) {
					return nil, types.ErrInvariantViolation.Wrapf("admin index %s/%d references an unknown exchange", key.K1(), key.K2())
				}
				return nil, err
			}
			if exchange.GetStatus() == types.ExchangeStatus_EXCHANGE_STATUS_DELETED {
				return nil, types.ErrInvariantViolation.Wrapf("deleted exchange %d remains in admin index", key.K2())
			}
			if exchange.GetAdminAddress() != key.K1() {
				return nil, types.ErrInvariantViolation.Wrapf("admin index %s/%d does not match exchange owner", key.K1(), key.K2())
			}
			return exchange, nil
		},
		sdkquery.WithCollectionPaginationPairPrefix[string, uint64](exchangeAdmin),
	)
	if err != nil {
		return nil, err
	}
	return &types.QueryExchangesByExchangeAdminResponse{Exchanges: exchanges, Pagination: page}, nil
}

func (q QueryServer) IsBexAdmin(ctx context.Context, req *types.QueryIsBexAdminRequest) (*types.QueryIsBexAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	isBexAdmin, err := q.keeper.IsAdmin(ctx, req.GetBexAdminAddress())
	if err != nil {
		return nil, err
	}
	return &types.QueryIsBexAdminResponse{IsBexAdmin: isBexAdmin}, nil
}

func (q QueryServer) ReserveDepositors(ctx context.Context, req *types.QueryReserveDepositorsRequest) (*types.QueryReserveDepositorsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if _, err := q.keeper.GetExchange(ctx, req.GetExchangeId()); err != nil {
		return nil, err
	}
	depositors, page, err := sdkquery.CollectionPaginate(
		ctx,
		q.keeper.reserveDepositors,
		req.GetPagination(),
		func(key collections.Pair[uint64, string], _ collections.NoValue) (string, error) {
			return key.K2(), nil
		},
		sdkquery.WithCollectionPaginationPairPrefix[uint64, string](req.GetExchangeId()),
	)
	if err != nil {
		return nil, err
	}
	return &types.QueryReserveDepositorsResponse{
		Depositors: depositors,
		Pagination: page,
	}, nil
}

func (q QueryServer) IsReserveDepositor(ctx context.Context, req *types.QueryIsReserveDepositorRequest) (*types.QueryIsReserveDepositorResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if _, err := q.keeper.GetExchange(ctx, req.GetExchangeId()); err != nil {
		return nil, err
	}
	isDepositor, err := q.keeper.IsReserveDepositor(ctx, req.GetExchangeId(), req.GetDepositorAddress())
	if err != nil {
		return nil, err
	}
	return &types.QueryIsReserveDepositorResponse{IsReserveDepositor: isDepositor}, nil
}

func (q QueryServer) CollectedFees(ctx context.Context, req *types.QueryFeesRequest) (*types.QueryFeesResponse, error) {
	return q.fees(ctx, req, q.keeper.GetCollectedFees)
}

func (q QueryServer) LockedFees(ctx context.Context, req *types.QueryFeesRequest) (*types.QueryFeesResponse, error) {
	return q.fees(ctx, req, q.keeper.GetLockedFees)
}

func (q QueryServer) AvailableFees(ctx context.Context, req *types.QueryFeesRequest) (*types.QueryFeesResponse, error) {
	return q.fees(ctx, req, q.keeper.GetAvailableFees)
}

func (q QueryServer) PendingLiabilities(
	ctx context.Context,
	req *types.QueryPendingLiabilitiesRequest,
) (*types.QueryPendingLiabilitiesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if _, err := q.keeper.GetExchange(ctx, req.GetExchangeId()); err != nil {
		return nil, err
	}
	coins, err := q.keeper.GetPendingLiabilities(ctx, req.GetExchangeId())
	if err != nil {
		return nil, err
	}
	return &types.QueryPendingLiabilitiesResponse{Ledger: coinsToLedger(coins)}, nil
}

func (q QueryServer) fees(ctx context.Context, req *types.QueryFeesRequest, get func(context.Context, uint64) (sdk.Coins, error)) (*types.QueryFeesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if _, err := q.keeper.GetExchange(ctx, req.GetExchangeId()); err != nil {
		return nil, err
	}
	coins, err := get(ctx, req.GetExchangeId())
	if err != nil {
		return nil, err
	}
	return &types.QueryFeesResponse{Ledger: coinsToLedger(coins)}, nil
}

func (q QueryServer) VolumeWindow(ctx context.Context, req *types.QueryVolumeWindowRequest) (*types.QueryVolumeWindowResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchange, err := q.keeper.GetExchange(ctx, req.GetExchangeId())
	if err != nil {
		return nil, err
	}
	amount, err := q.keeper.GetCurrentVolumeAmount(ctx, exchange, req.GetDirection())
	if err != nil {
		return nil, err
	}
	_, _, _, _, cap, err := quoteConfig(exchange, req.GetDirection())
	if err != nil {
		return nil, err
	}
	blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	epochSeconds, generation, err := effectiveVolumeWindowIdentity(exchange, blockTime)
	if err != nil {
		return nil, err
	}
	key := currentVolumeKey(
		blockTime,
		exchange.GetId(),
		req.GetDirection(),
		epochSeconds,
		generation,
	)
	epochStart, _ := volumeWindowEpochStart(key)
	return &types.QueryVolumeWindowResponse{
		Window: &types.VolumeWindow{
			ExchangeId:             exchange.GetId(),
			Direction:              req.GetDirection(),
			EpochStartUnix:         epochStart,
			EpochSeconds:           volumeWindowEpochSeconds(key),
			Amount:                 amount.String(),
			VolumeWindowGeneration: volumeWindowGeneration(key),
		},
		Cap: cap.String(),
	}, nil
}

func (q QueryServer) QuoteSwap(ctx context.Context, req *types.QueryQuoteSwapRequest) (*types.QueryQuoteSwapResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	quote, err := q.keeper.QuoteSwap(ctx, &types.QuoteSwapRequest{
		ExchangeId: req.GetExchangeId(),
		InputDenom: req.GetInputDenom(),
		AmountIn:   req.GetAmountIn(),
	})
	if err != nil {
		return nil, err
	}
	return &types.QueryQuoteSwapResponse{Quote: quote}, nil
}
