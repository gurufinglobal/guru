package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

var _ types.QueryServer = (*QueryServer)(nil)

// QueryServer implements the read-only feepolicy service.
type QueryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

func NewQueryServer(keeper *Keeper) *QueryServer {
	if keeper == nil {
		panic("feepolicy query server keeper cannot be nil")
	}
	return &QueryServer{keeper: keeper}
}

func (s *QueryServer) ModeratorAddress(
	ctx context.Context,
	_ *types.QueryModeratorAddressRequest,
) (*types.QueryModeratorAddressResponse, error) {
	moderator, err := s.keeper.GetModeratorAddress(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrInvalidRequest.Wrap("moderator address not found")
		}
		return nil, err
	}
	return &types.QueryModeratorAddressResponse{ModeratorAddress: moderator}, nil
}

func (s *QueryServer) Discounts(
	ctx context.Context,
	req *types.QueryDiscountsRequest,
) (*types.QueryDiscountsResponse, error) {
	if req == nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("request cannot be nil")
	}
	discounts, pageResponse, err := s.keeper.GetPaginatedDiscounts(ctx, req.GetPagination())
	if err != nil {
		return nil, err
	}
	return &types.QueryDiscountsResponse{Discounts: discounts, Pagination: pageResponse}, nil
}

func (s *QueryServer) Discount(
	ctx context.Context,
	req *types.QueryDiscountRequest,
) (*types.QueryDiscountResponse, error) {
	if req == nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("request cannot be nil")
	}
	discount, found, err := s.keeper.GetAccountDiscounts(ctx, req.Address)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryDiscountResponse{}, nil
	}
	return &types.QueryDiscountResponse{Discount: discount}, nil
}
