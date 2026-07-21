package keeper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	querytypes "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type refundRouteQueryServer struct {
	types.UnimplementedQueryServer
	called         string
	refundID       string
	refundsRequest *types.QueryRefundsRequest
}

func (s *refundRouteQueryServer) Refund(_ context.Context, request *types.QueryRefundRequest) (*types.QueryRefundResponse, error) {
	s.called = "refund"
	s.refundID = request.GetRefundId()
	return &types.QueryRefundResponse{}, nil
}

func (s *refundRouteQueryServer) Refunds(_ context.Context, request *types.QueryRefundsRequest) (*types.QueryRefundsResponse, error) {
	s.called = "refunds"
	requestCopy := *request
	if request.Pagination != nil {
		paginationCopy := *request.Pagination
		paginationCopy.Key = append([]byte(nil), request.Pagination.Key...)
		requestCopy.Pagination = &paginationCopy
	}
	s.refundsRequest = &requestCopy
	return &types.QueryRefundsResponse{}, nil
}

func TestRefundCollectionRESTRoutePrecedesWildcard(t *testing.T) {
	mux := runtime.NewServeMux()
	server := &refundRouteQueryServer{}
	require.NoError(t, types.RegisterQueryHandlerServer(context.Background(), mux, server))

	request := httptest.NewRequest(
		http.MethodGet,
		"/guru/transwap/v1/refunds?status=REFUND_STATUS_PENDING&pagination.limit=7",
		nil,
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "refunds", server.called)
	require.Equal(t, types.RefundStatus_REFUND_STATUS_PENDING, server.refundsRequest.GetStatus())
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
	completed := types.CloneRefundRecord(pending)
	completed.Id = RefundID(types.PortID, "channel-7", exchangeAtomicSequence+1)
	completed.OriginalOutputSequence = exchangeAtomicSequence + 1
	completed.Status = types.RefundStatus_REFUND_STATUS_COMPLETED
	require.NoError(t, state.keeper.SetRefundRecord(state.ctx, completed))

	manual := types.CloneRefundRecord(pending)
	manual.Id = RefundID(types.PortID, "channel-7", exchangeAtomicSequence+2)
	manual.OriginalOutputSequence = exchangeAtomicSequence + 2
	manual.Status = types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE
	manual.RetryCount = types.DefaultMaxRefundRetries
	manual.Receiver = state.receiver.String()
	manual.ClaimAddress = state.receiver.String()
	require.NoError(t, state.keeper.SetRefundRecord(state.ctx, manual))

	byStatus, err := state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{
		Status: types.RefundStatus_REFUND_STATUS_COMPLETED,
		Pagination: &querytypes.PageRequest{
			Limit:      10,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, byStatus.GetRefunds(), 1)
	require.Equal(t, completed.GetId(), byStatus.GetRefunds()[0].GetId())
	require.Equal(t, uint64(1), byStatus.GetPagination().GetTotal())

	byReceiver, err := state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{
		Receiver: state.sender.String(),
		Pagination: &querytypes.PageRequest{
			Limit:      10,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, byReceiver.GetRefunds(), 2)
	require.Equal(t, uint64(2), byReceiver.GetPagination().GetTotal())

	filteredFirstPage, err := state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{
		Receiver: state.sender.String(),
		Pagination: &querytypes.PageRequest{
			Limit: 1,
		},
	})
	require.NoError(t, err)
	require.Len(t, filteredFirstPage.GetRefunds(), 1)
	require.NotEmpty(t, filteredFirstPage.GetPagination().GetNextKey())
	filteredSecondPage, err := state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{
		Receiver: state.sender.String(),
		Pagination: &querytypes.PageRequest{
			Key:   filteredFirstPage.GetPagination().GetNextKey(),
			Limit: 1,
		},
	})
	require.NoError(t, err)
	require.Len(t, filteredSecondPage.GetRefunds(), 1)
	require.NotEqual(t, filteredFirstPage.GetRefunds()[0].GetId(), filteredSecondPage.GetRefunds()[0].GetId())

	firstPage, err := state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{
		Pagination: &querytypes.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, firstPage.GetRefunds(), 1)
	require.NotEmpty(t, firstPage.GetPagination().GetNextKey())
	secondPage, err := state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{
		Pagination: &querytypes.PageRequest{
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
	_, err = state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{
		Status: types.RefundStatus(99),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = state.keeper.Refunds(state.ctx, &types.QueryRefundsRequest{Receiver: "not-bech32"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
