package keeper

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

var _ transwapv1.QueryServer = (*Keeper)(nil)

func (k Keeper) Params(goCtx context.Context, req *transwapv1.QueryParamsRequest) (*transwapv1.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params, err := k.GetParams(goCtx)
	if err != nil {
		return nil, err
	}
	return &transwapv1.QueryParamsResponse{Params: params}, nil
}

func (k Keeper) Refund(goCtx context.Context, req *transwapv1.QueryRefundRequest) (*transwapv1.QueryRefundResponse, error) {
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
	return &transwapv1.QueryRefundResponse{Refund: refund}, nil
}

// Refunds returns refund records in stable store-key order. Filtering is
// applied before pagination, so count_total and next_key describe the filtered
// result set rather than all refund records.
func (k Keeper) Refunds(goCtx context.Context, req *transwapv1.QueryRefundsRequest) (*transwapv1.QueryRefundsResponse, error) {
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
	refunds := make([]*transwapv1.RefundRecord, 0)
	pageRes, err := sdkquery.FilteredPaginate(
		k.refundRecordStore(ctx),
		pulsarPageRequestToSDK(req.GetPagination()),
		func(_, value []byte, accumulate bool) (bool, error) {
			record := &transwapv1.RefundRecord{}
			if err := k.cdc.Unmarshal(value, record); err != nil {
				return false, err
			}
			if req.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_UNSPECIFIED &&
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

	return &transwapv1.QueryRefundsResponse{
		Refunds:    refunds,
		Pagination: sdkPageResponseToPulsar(pageRes),
	}, nil
}

func validRefundStatusFilter(refundStatus transwapv1.RefundStatus) bool {
	switch refundStatus {
	case transwapv1.RefundStatus_REFUND_STATUS_UNSPECIFIED,
		transwapv1.RefundStatus_REFUND_STATUS_PENDING,
		transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT,
		transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE,
		transwapv1.RefundStatus_REFUND_STATUS_COMPLETED,
		transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE,
		transwapv1.RefundStatus_REFUND_STATUS_CLAIMED:
		return true
	default:
		return false
	}
}

// Denom implements the Query/Denom gRPC method
func (k Keeper) Denom(goCtx context.Context, req *transwapv1.QueryDenomRequest) (*transwapv1.QueryDenomResponse, error) {
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

	return &transwapv1.QueryDenomResponse{
		Denom: denom,
	}, nil
}

// Denoms implements the Query/Denoms gRPC method
func (k Keeper) Denoms(ctx context.Context, req *transwapv1.QueryDenomsRequest) (*transwapv1.QueryDenomsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	var denoms types.Denoms
	store := prefix.NewStore(runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx)), types.DenomKey)

	pageRes, err := sdkquery.Paginate(store, pulsarPageRequestToSDK(req.Pagination), func(_, value []byte) error {
		denom := &transwapv1.Denom{}
		if err := k.cdc.Unmarshal(value, denom); err != nil {
			return err
		}

		denoms = append(denoms, denom)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &transwapv1.QueryDenomsResponse{
		Denoms:     denoms.Sort(),
		Pagination: sdkPageResponseToPulsar(pageRes),
	}, nil
}

// DenomHash implements the Query/DenomHash gRPC method
func (k Keeper) DenomHash(goCtx context.Context, req *transwapv1.QueryDenomHashRequest) (*transwapv1.QueryDenomHashResponse, error) {
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

	return &transwapv1.QueryDenomHashResponse{
		Hash: denomHash.String(),
	}, nil
}

// EscrowAddress implements the EscrowAddress gRPC method
func (k Keeper) EscrowAddress(goCtx context.Context, req *transwapv1.QueryEscrowAddressRequest) (*transwapv1.QueryEscrowAddressResponse, error) {
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

	return &transwapv1.QueryEscrowAddressResponse{
		EscrowAddress: addr.String(),
	}, nil
}

// TotalEscrowForDenom implements the TotalEscrowForDenom gRPC method.
func (k Keeper) TotalEscrowForDenom(goCtx context.Context, req *transwapv1.QueryTotalEscrowForDenomRequest) (*transwapv1.QueryTotalEscrowForDenomResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	amount := k.GetTotalEscrowForDenom(ctx, req.Denom)

	return &transwapv1.QueryTotalEscrowForDenomResponse{
		Amount: types.SDKCoinToProto(amount),
	}, nil
}

func pulsarPageRequestToSDK(pageReq *queryv1beta1.PageRequest) *sdkquery.PageRequest {
	if pageReq == nil {
		return nil
	}

	return &sdkquery.PageRequest{
		Key:        pageReq.GetKey(),
		Offset:     pageReq.GetOffset(),
		Limit:      pageReq.GetLimit(),
		CountTotal: pageReq.GetCountTotal(),
		Reverse:    pageReq.GetReverse(),
	}
}

func sdkPageResponseToPulsar(pageRes *sdkquery.PageResponse) *queryv1beta1.PageResponse {
	if pageRes == nil {
		return nil
	}

	return &queryv1beta1.PageResponse{
		NextKey: pageRes.GetNextKey(),
		Total:   pageRes.GetTotal(),
	}
}
