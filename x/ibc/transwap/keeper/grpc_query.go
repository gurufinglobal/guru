package keeper

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"

	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/runtime"
	"cosmossdk.io/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

var _ types.QueryServer = (*Keeper)(nil)

func (k Keeper) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params, err := k.GetParams(goCtx)
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

func (k Keeper) Refund(goCtx context.Context, req *types.QueryRefundRequest) (*types.QueryRefundResponse, error) {
	if req == nil || strings.TrimSpace(req.GetRefundId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "refund id is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	refund, found, err := k.GetRefundRecord(ctx, req.GetRefundId())
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, status.Error(codes.NotFound, types.ErrRefundNotFound.Wrap(req.GetRefundId()).Error())
	}
	return &types.QueryRefundResponse{Refund: refund}, nil
}

// Refunds returns refund records in stable store-key order. Filtering is
// applied before pagination, so count_total and next_key describe the filtered
// result set rather than all refund records.
func (k Keeper) Refunds(goCtx context.Context, req *types.QueryRefundsRequest) (*types.QueryRefundsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	if !validRefundStatusFilter(req.GetStatus()) {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported refund status %d", req.GetStatus())
	}

	receiver := req.GetReceiver()
	if receiver != "" {
		if receiver != strings.TrimSpace(receiver) {
			return nil, status.Error(codes.InvalidArgument, "refund receiver must not contain surrounding whitespace")
		}
		_, address, err := bech32.DecodeAndConvert(receiver)
		if err != nil || sdk.VerifyAddressFormat(address) != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid refund receiver")
		}
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	refunds := make([]*types.RefundRecord, 0)
	pageRes, err := sdkquery.FilteredPaginate(
		k.refundRecordStore(ctx),
		req.GetPagination(),
		func(_, value []byte, accumulate bool) (bool, error) {
			record := &types.RefundRecord{}
			if err := k.cdc.Unmarshal(value, record); err != nil {
				return false, err
			}
			if req.GetStatus() != types.RefundStatus_REFUND_STATUS_UNSPECIFIED &&
				record.GetStatus() != req.GetStatus() {
				return false, nil
			}
			if receiver != "" && record.GetReceiver() != receiver {
				return false, nil
			}
			if accumulate {
				refunds = append(refunds, record)
			}
			return true, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryRefundsResponse{
		Refunds:    refunds,
		Pagination: pageRes,
	}, nil
}

func validRefundStatusFilter(refundStatus types.RefundStatus) bool {
	switch refundStatus {
	case types.RefundStatus_REFUND_STATUS_UNSPECIFIED,
		types.RefundStatus_REFUND_STATUS_PENDING,
		types.RefundStatus_REFUND_STATUS_IN_FLIGHT,
		types.RefundStatus_REFUND_STATUS_RETRYABLE,
		types.RefundStatus_REFUND_STATUS_COMPLETED,
		types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE,
		types.RefundStatus_REFUND_STATUS_CLAIMED:
		return true
	default:
		return false
	}
}

// Denom implements the Query/Denom gRPC method
func (k Keeper) Denom(goCtx context.Context, req *types.QueryDenomRequest) (*types.QueryDenomResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	hash, err := types.ParseHexHash(strings.TrimPrefix(req.Hash, "ibc/"))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid denom trace hash: %s, error: %s", hash.String(), err))
	}

	denom, found := k.GetDenom(ctx, hash)
	if !found {
		return nil, status.Error(
			codes.NotFound,
			errorsmod.Wrap(types.ErrDenomNotFound, req.Hash).Error(),
		)
	}

	return &types.QueryDenomResponse{
		Denom: &denom,
	}, nil
}

// Denoms implements the Query/Denoms gRPC method
func (k Keeper) Denoms(ctx context.Context, req *types.QueryDenomsRequest) (*types.QueryDenomsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	var denoms types.Denoms
	store := prefix.NewStore(runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx)), types.DenomKey)

	pageRes, err := sdkquery.Paginate(store, req.Pagination, func(_, value []byte) error {
		var denom types.Denom
		if err := k.cdc.Unmarshal(value, &denom); err != nil {
			return err
		}

		denoms = append(denoms, denom)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryDenomsResponse{
		Denoms:     denoms.Sort(),
		Pagination: pageRes,
	}, nil
}

// DenomHash implements the Query/DenomHash gRPC method
func (k Keeper) DenomHash(goCtx context.Context, req *types.QueryDenomHashRequest) (*types.QueryDenomHashResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Convert given request trace path to Denom struct to confirm the path in a valid denom trace format
	denom := types.ExtractDenomFromPath(req.Trace)
	if err := types.ValidateDenom(denom); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	denomHash := types.DenomHash(denom)
	found := k.HasDenom(ctx, denomHash)
	if !found {
		return nil, status.Error(
			codes.NotFound,
			errorsmod.Wrap(types.ErrDenomNotFound, req.Trace).Error(),
		)
	}

	return &types.QueryDenomHashResponse{
		Hash: denomHash.String(),
	}, nil
}

// EscrowAddress implements the EscrowAddress gRPC method
func (k Keeper) EscrowAddress(goCtx context.Context, req *types.QueryEscrowAddressRequest) (*types.QueryEscrowAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	addr := types.GetEscrowAddress(req.PortId, req.ChannelId)

	// if err := validate.GRPCRequest(req.PortId, req.ChannelId); err != nil {
	// 	return nil, err
	// }

	if !k.channelKeeper.HasChannel(ctx, req.PortId, req.ChannelId) {
		return nil, status.Error(
			codes.NotFound,
			errorsmod.Wrapf(channeltypes.ErrChannelNotFound, "port ID (%s) channel ID (%s)", req.PortId, req.ChannelId).Error(),
		)
	}

	return &types.QueryEscrowAddressResponse{
		EscrowAddress: addr.String(),
	}, nil
}

// TotalEscrowForDenom implements the TotalEscrowForDenom gRPC method.
func (k Keeper) TotalEscrowForDenom(goCtx context.Context, req *types.QueryTotalEscrowForDenomRequest) (*types.QueryTotalEscrowForDenomResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	amount := k.GetTotalEscrowForDenom(ctx, req.Denom)

	return &types.QueryTotalEscrowForDenomResponse{Amount: amount}, nil
}
