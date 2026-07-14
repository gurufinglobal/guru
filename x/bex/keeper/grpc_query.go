package keeper

import (
	"context"
	"errors"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

var _ bexv1.QueryServer = QueryServer{}

type QueryServer struct {
	bexv1.UnimplementedQueryServer
	keeper *Keeper
}

func NewQueryServer(keeper *Keeper) QueryServer {
	return QueryServer{keeper: keeper}
}

func (q QueryServer) Exchange(ctx context.Context, req *bexv1.QueryExchangeRequest) (*bexv1.QueryExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchange, err := q.keeper.GetExchange(ctx, req.GetExchangeId())
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryExchangeResponse{Exchange: exchange}, nil
}

func (q QueryServer) Exchanges(ctx context.Context, req *bexv1.QueryExchangesRequest) (*bexv1.QueryExchangesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	pageRequest := sdkPageRequest(req.GetPagination())
	exchanges, page, err := sdkquery.CollectionFilteredPaginate(
		ctx,
		q.keeper.exchanges,
		pageRequest,
		func(_ uint64, exchange *bexv1.Exchange) (bool, error) {
			return req.GetIncludeDeleted() || exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED, nil
		},
		func(_ uint64, exchange *bexv1.Exchange) (*bexv1.Exchange, error) {
			return exchange, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryExchangesResponse{Exchanges: exchanges, Pagination: apiPageResponse(page)}, nil
}

func (q QueryServer) ExchangesByExchangeAdmin(ctx context.Context, req *bexv1.QueryExchangesByExchangeAdminRequest) (*bexv1.QueryExchangesByExchangeAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchangeAdmin, _, err := q.keeper.canonicalAddress(req.GetExchangeAdminAddress())
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrapf("invalid exchange admin address: %v", err)
	}
	pageRequest := sdkPageRequest(req.GetPagination())
	exchanges, page, err := sdkquery.CollectionPaginate(
		ctx,
		q.keeper.exchangesByAdmin,
		pageRequest,
		func(key collections.Pair[string, uint64], _ collections.NoValue) (*bexv1.Exchange, error) {
			exchange, err := q.keeper.GetExchange(ctx, key.K2())
			if err != nil {
				if errors.Is(err, types.ErrExchangeNotFound) {
					return nil, types.ErrInvariantViolation.Wrapf("admin index %s/%d references an unknown exchange", key.K1(), key.K2())
				}
				return nil, err
			}
			if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
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
	return &bexv1.QueryExchangesByExchangeAdminResponse{Exchanges: exchanges, Pagination: apiPageResponse(page)}, nil
}

func (q QueryServer) IsBexAdmin(ctx context.Context, req *bexv1.QueryIsBexAdminRequest) (*bexv1.QueryIsBexAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	isBexAdmin, err := q.keeper.IsAdmin(ctx, req.GetBexAdminAddress())
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryIsBexAdminResponse{IsBexAdmin: isBexAdmin}, nil
}

func (q QueryServer) ReserveDepositors(ctx context.Context, req *bexv1.QueryReserveDepositorsRequest) (*bexv1.QueryReserveDepositorsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if _, err := q.keeper.GetExchange(ctx, req.GetExchangeId()); err != nil {
		return nil, err
	}
	pageRequest := sdkPageRequest(req.GetPagination())
	depositors, page, err := sdkquery.CollectionPaginate(
		ctx,
		q.keeper.reserveDepositors,
		pageRequest,
		func(key collections.Pair[uint64, string], _ collections.NoValue) (string, error) {
			return key.K2(), nil
		},
		sdkquery.WithCollectionPaginationPairPrefix[uint64, string](req.GetExchangeId()),
	)
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryReserveDepositorsResponse{
		Depositors: depositors,
		Pagination: apiPageResponse(page),
	}, nil
}

func (q QueryServer) IsReserveDepositor(ctx context.Context, req *bexv1.QueryIsReserveDepositorRequest) (*bexv1.QueryIsReserveDepositorResponse, error) {
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
	return &bexv1.QueryIsReserveDepositorResponse{IsReserveDepositor: isDepositor}, nil
}

func (q QueryServer) CollectedFees(ctx context.Context, req *bexv1.QueryFeesRequest) (*bexv1.QueryFeesResponse, error) {
	return q.fees(ctx, req, q.keeper.GetCollectedFees)
}

func (q QueryServer) LockedFees(ctx context.Context, req *bexv1.QueryFeesRequest) (*bexv1.QueryFeesResponse, error) {
	return q.fees(ctx, req, q.keeper.GetLockedFees)
}

func (q QueryServer) AvailableFees(ctx context.Context, req *bexv1.QueryFeesRequest) (*bexv1.QueryFeesResponse, error) {
	return q.fees(ctx, req, q.keeper.GetAvailableFees)
}

func (q QueryServer) fees(ctx context.Context, req *bexv1.QueryFeesRequest, get func(context.Context, uint64) (sdk.Coins, error)) (*bexv1.QueryFeesResponse, error) {
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
	return &bexv1.QueryFeesResponse{Ledger: coinsToLedger(coins)}, nil
}

func (q QueryServer) VolumeWindow(ctx context.Context, req *bexv1.QueryVolumeWindowRequest) (*bexv1.QueryVolumeWindowResponse, error) {
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
	key := currentVolumeKey(
		sdk.UnwrapSDKContext(ctx).BlockTime(),
		exchange.GetId(),
		req.GetDirection(),
		effectiveVolumeEpochSeconds(exchange, sdk.UnwrapSDKContext(ctx).BlockTime()),
	)
	return &bexv1.QueryVolumeWindowResponse{
		Window: &bexv1.VolumeWindow{
			ExchangeId:     exchange.GetId(),
			Direction:      req.GetDirection(),
			EpochStartUnix: key.K3(),
			EpochSeconds:   key.K4(),
			Amount:         amount.String(),
		},
		Cap: cap.String(),
	}, nil
}

func (q QueryServer) QuoteSwap(ctx context.Context, req *bexv1.QueryQuoteSwapRequest) (*bexv1.QueryQuoteSwapResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	quote, err := q.keeper.QuoteSwap(ctx, &bexv1.QuoteSwapRequest{
		ExchangeId: req.GetExchangeId(),
		InputDenom: req.GetInputDenom(),
		AmountIn:   req.GetAmountIn(),
	})
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryQuoteSwapResponse{Quote: quote}, nil
}

func sdkPageRequest(req *queryv1beta1.PageRequest) *sdkquery.PageRequest {
	if req == nil {
		return nil
	}
	return &sdkquery.PageRequest{
		Key:        append([]byte(nil), req.GetKey()...),
		Offset:     req.GetOffset(),
		Limit:      req.GetLimit(),
		CountTotal: req.GetCountTotal(),
		Reverse:    req.GetReverse(),
	}
}

func apiPageResponse(response *sdkquery.PageResponse) *queryv1beta1.PageResponse {
	if response == nil {
		return &queryv1beta1.PageResponse{}
	}
	return &queryv1beta1.PageResponse{
		NextKey: append([]byte(nil), response.GetNextKey()...),
		Total:   response.GetTotal(),
	}
}
