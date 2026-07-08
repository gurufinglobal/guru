package keeper

import (
	"context"
	"strconv"
	"time"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
	pager, err := newPager(req.GetPagination())
	if err != nil {
		return nil, err
	}
	exchanges := []*bexv1.Exchange{}
	seen := uint64(0)
	err = q.keeper.exchanges.Walk(ctx, nil, func(_ uint64, exchange *bexv1.Exchange) (bool, error) {
		if !req.GetIncludeDeleted() && exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
			return false, nil
		}
		if seen < pager.offset {
			seen++
			return false, nil
		}
		if uint64(len(exchanges)) >= pager.limit {
			return true, nil
		}
		exchanges = append(exchanges, exchange)
		seen++
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryExchangesResponse{Exchanges: exchanges, Pagination: pager.response(seen, uint64(len(exchanges)))}, nil
}

func (q QueryServer) ExchangesByAdmin(ctx context.Context, req *bexv1.QueryExchangesByAdminRequest) (*bexv1.QueryExchangesByAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	admin, _, err := q.keeper.canonicalAddress(req.GetAdminAddress())
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrapf("invalid admin address: %v", err)
	}
	pager, err := newPager(req.GetPagination())
	if err != nil {
		return nil, err
	}
	exchanges := []*bexv1.Exchange{}
	seen := uint64(0)
	rng := collections.NewPrefixedPairRange[string, uint64](admin)
	err = q.keeper.exchangesByAdmin.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		if seen < pager.offset {
			seen++
			return false, nil
		}
		if uint64(len(exchanges)) >= pager.limit {
			return true, nil
		}
		exchange, err := q.keeper.GetExchange(ctx, key.K2())
		if err != nil {
			return false, err
		}
		if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
			exchanges = append(exchanges, exchange)
		}
		seen++
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryExchangesByAdminResponse{Exchanges: exchanges, Pagination: pager.response(seen, uint64(len(exchanges)))}, nil
}

func (q QueryServer) IsAdmin(ctx context.Context, req *bexv1.QueryIsAdminRequest) (*bexv1.QueryIsAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	isAdmin, err := q.keeper.IsAdmin(ctx, req.GetAdminAddress())
	if err != nil {
		return nil, err
	}
	return &bexv1.QueryIsAdminResponse{IsAdmin: isAdmin}, nil
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

func (q QueryServer) ExchangeReadiness(ctx context.Context, req *bexv1.QueryExchangeReadinessRequest) (*bexv1.QueryExchangeReadinessResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	readiness := &bexv1.ExchangeReadinessResponse{
		ExchangeId: req.GetExchangeId(),
		Direction:  req.GetDirection(),
		Ready:      true,
	}
	exchange, err := q.keeper.GetActiveExchange(ctx, req.GetExchangeId())
	if err != nil {
		readiness.Ready = false
		readiness.BlockingReasons = append(readiness.BlockingReasons, err.Error())
		return &bexv1.QueryExchangeReadinessResponse{Readiness: readiness}, nil
	}
	if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
		readiness.Ready = false
		readiness.BlockingReasons = append(readiness.BlockingReasons, "exchange is not active")
	} else if err := q.keeper.validateActiveRoutes(ctx, exchange); err != nil {
		readiness.Ready = false
		readiness.BlockingReasons = append(readiness.BlockingReasons, err.Error())
	}
	symbol, _, _, _, _, err := quoteConfig(exchange, req.GetDirection())
	if err != nil {
		readiness.Ready = false
		readiness.BlockingReasons = append(readiness.BlockingReasons, err.Error())
	} else {
		rateValue, err := q.keeper.oracleKeeper.GetLatestValue(ctx, symbol)
		if err != nil {
			readiness.Ready = false
			readiness.BlockingReasons = append(readiness.BlockingReasons, "oracle value unavailable")
		} else {
			rate, err := sdkmath.LegacyNewDecFromStr(rateValue.GetValue())
			if err != nil || !rate.IsPositive() {
				readiness.Ready = false
				readiness.BlockingReasons = append(readiness.BlockingReasons, "oracle value is not a positive decimal")
			}
			if exchange.GetMaxOracleStalenessSeconds() > 0 {
				blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()
				if blockTime.IsZero() {
					blockTime = time.Unix(0, 0)
				}
				if rateValue.GetBlockTimeUnix() <= 0 || blockTime.Unix()-rateValue.GetBlockTimeUnix() > int64(exchange.GetMaxOracleStalenessSeconds()) {
					readiness.Ready = false
					readiness.BlockingReasons = append(readiness.BlockingReasons, "oracle value is stale")
				}
			}
		}
	}
	return &bexv1.QueryExchangeReadinessResponse{Readiness: readiness}, nil
}

type pager struct {
	offset uint64
	limit  uint64
}

func newPager(req *queryv1beta1.PageRequest) (pager, error) {
	p := pager{limit: defaultPageLimit}
	if req == nil {
		return p, nil
	}
	if key := req.GetKey(); len(key) > 0 {
		offset, err := strconv.ParseUint(string(key), 10, 64)
		if err != nil {
			return p, types.ErrInvalidRequest.Wrap("invalid pagination key")
		}
		p.offset = offset
	} else {
		p.offset = req.GetOffset()
	}
	if req.GetLimit() > 0 {
		p.limit = req.GetLimit()
	}
	if p.limit > maxPageLimit {
		p.limit = maxPageLimit
	}
	return p, nil
}

func (p pager) response(seen, count uint64) *queryv1beta1.PageResponse {
	next := p.offset + count
	if count == p.limit && seen >= next {
		return &queryv1beta1.PageResponse{NextKey: []byte(strconv.FormatUint(next, 10))}
	}
	return &queryv1beta1.PageResponse{}
}
