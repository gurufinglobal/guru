package keeper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type refundRouteQueryServer struct {
	transwapv1.UnimplementedQueryServer
	called         string
	refundID       string
	refundsRequest *transwapv1.QueryRefundsRequest
}

func (s *refundRouteQueryServer) Refund(_ context.Context, request *transwapv1.QueryRefundRequest) (*transwapv1.QueryRefundResponse, error) {
	s.called = "refund"
	s.refundID = request.GetRefundId()
	return &transwapv1.QueryRefundResponse{}, nil
}

func (s *refundRouteQueryServer) Refunds(_ context.Context, request *transwapv1.QueryRefundsRequest) (*transwapv1.QueryRefundsResponse, error) {
	s.called = "refunds"
	s.refundsRequest = proto.Clone(request).(*transwapv1.QueryRefundsRequest)
	return &transwapv1.QueryRefundsResponse{}, nil
}

func TestRefundCollectionRESTRoutePrecedesWildcard(t *testing.T) {
	mux := runtime.NewServeMux()
	server := &refundRouteQueryServer{}
	require.NoError(t, transwapv1.RegisterQueryHandlerServer(context.Background(), mux, server))

	request := httptest.NewRequest(
		http.MethodGet,
		"/guru/transwap/v1/refunds?status=REFUND_STATUS_PENDING&pagination.limit=7",
		nil,
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "refunds", server.called)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_PENDING, server.refundsRequest.GetStatus())
	require.Equal(t, uint64(7), server.refundsRequest.GetPagination().GetLimit())

	server.called = ""
	request = httptest.NewRequest(http.MethodGet, "/guru/transwap/v1/refunds/transwap/channel-7/42", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "refund", server.called)
	require.Equal(t, "transwap/channel-7/42", server.refundID)
}

func TestRefundsQueryFiltersBeforePagination(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	require.NoError(t, state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed positive test time.
	))

	pendingID := RefundID(types.PortID, "channel-7", exchangeAtomicSequence)
	pending := mustRefundRecord(t, state, pendingID)
	completed := proto.Clone(pending).(*transwapv1.RefundRecord)
	completed.Id = RefundID(types.PortID, "channel-7", exchangeAtomicSequence+1)
	completed.OriginalOutputSequence = exchangeAtomicSequence + 1
	completed.Status = transwapv1.RefundStatus_REFUND_STATUS_COMPLETED
	require.NoError(t, state.keeper.SetRefundRecord(state.ctx, completed))

	manual := proto.Clone(pending).(*transwapv1.RefundRecord)
	manual.Id = RefundID(types.PortID, "channel-7", exchangeAtomicSequence+2)
	manual.OriginalOutputSequence = exchangeAtomicSequence + 2
	manual.Status = transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE
	manual.RetryCount = types.DefaultMaxRefundRetries
	manual.Receiver = state.receiver.String()
	manual.ClaimAddress = state.receiver.String()
	require.NoError(t, state.keeper.SetRefundRecord(state.ctx, manual))

	byStatus, err := state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{
		Status: transwapv1.RefundStatus_REFUND_STATUS_COMPLETED,
		Pagination: &queryv1beta1.PageRequest{
			Limit:      10,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, byStatus.GetRefunds(), 1)
	require.Equal(t, completed.GetId(), byStatus.GetRefunds()[0].GetId())
	require.Equal(t, uint64(1), byStatus.GetPagination().GetTotal())

	byReceiver, err := state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{
		Receiver: state.sender.String(),
		Pagination: &queryv1beta1.PageRequest{
			Limit:      10,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, byReceiver.GetRefunds(), 2)
	require.Equal(t, uint64(2), byReceiver.GetPagination().GetTotal())

	filteredFirstPage, err := state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{
		Receiver: state.sender.String(),
		Pagination: &queryv1beta1.PageRequest{
			Limit: 1,
		},
	})
	require.NoError(t, err)
	require.Len(t, filteredFirstPage.GetRefunds(), 1)
	require.NotEmpty(t, filteredFirstPage.GetPagination().GetNextKey())
	filteredSecondPage, err := state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{
		Receiver: state.sender.String(),
		Pagination: &queryv1beta1.PageRequest{
			Key:   filteredFirstPage.GetPagination().GetNextKey(),
			Limit: 1,
		},
	})
	require.NoError(t, err)
	require.Len(t, filteredSecondPage.GetRefunds(), 1)
	require.NotEqual(t, filteredFirstPage.GetRefunds()[0].GetId(), filteredSecondPage.GetRefunds()[0].GetId())

	firstPage, err := state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{
		Pagination: &queryv1beta1.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, firstPage.GetRefunds(), 1)
	require.NotEmpty(t, firstPage.GetPagination().GetNextKey())
	secondPage, err := state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{
		Pagination: &queryv1beta1.PageRequest{
			Key:   firstPage.GetPagination().GetNextKey(),
			Limit: 1,
		},
	})
	require.NoError(t, err)
	require.Len(t, secondPage.GetRefunds(), 1)
	require.NotEqual(t, firstPage.GetRefunds()[0].GetId(), secondPage.GetRefunds()[0].GetId())
}

func TestRefundsQueryRejectsInvalidFilters(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)

	_, err := state.keeper.Refunds(state.ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{
		Status: transwapv1.RefundStatus(99),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = state.keeper.Refunds(state.ctx, &transwapv1.QueryRefundsRequest{Receiver: "not-bech32"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
