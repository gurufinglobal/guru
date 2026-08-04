package service

import (
	"context"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxConsumerSymbols = 256

type ConsumerServer struct {
	oraclev1.UnimplementedOracleSidecarServer

	state            *State
	maxRequestBytes  uint32
	maxResponseBytes uint32
}

func NewConsumerServer(state *State, maxRequestBytes, maxResponseBytes uint32) *ConsumerServer {
	return &ConsumerServer{
		state:            state,
		maxRequestBytes:  maxRequestBytes,
		maxResponseBytes: maxResponseBytes,
	}
}

func (s *ConsumerServer) GetAggregates(_ context.Context, request *oraclev1.GetAggregatesRequest) (*oraclev1.GetAggregatesResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(request.Symbols) > maxConsumerSymbols {
		return nil, status.Error(codes.ResourceExhausted, "symbol count exceeds limit")
	}
	if request.Size() > int(s.maxRequestBytes) {
		return nil, status.Error(codes.ResourceExhausted, "request exceeds byte limit")
	}
	for i, symbol := range request.Symbols {
		normalized, err := domain.NormalizeSymbol(symbol)
		if err != nil || normalized != symbol {
			return nil, status.Error(codes.InvalidArgument, "symbols must be canonical")
		}
		if i > 0 && request.Symbols[i-1] >= symbol {
			return nil, status.Error(codes.InvalidArgument, "symbols must be sorted and unique")
		}
	}
	values := s.state.Fresh(request.Symbols)
	results := make([]*oraclev1.AggregatedResult, 0, len(values))
	for _, value := range values {
		results = append(results, &oraclev1.AggregatedResult{
			Symbol:      value.Symbol,
			Value:       value.Value,
			SourceCount: value.SourceCount,
		})
	}
	response := &oraclev1.GetAggregatesResponse{Results: results}
	if response.Size() > int(s.maxResponseBytes) {
		return nil, status.Error(codes.ResourceExhausted, "response exceeds byte limit")
	}
	return response, nil
}
