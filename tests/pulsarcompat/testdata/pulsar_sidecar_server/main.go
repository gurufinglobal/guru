package main

import (
	"context"
	"fmt"
	"net"
	"os"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"google.golang.org/grpc"
)

type server struct {
	oraclev1.UnimplementedOracleSidecarServer
}

func (server) GetAggregates(
	_ context.Context,
	request *oraclev1.GetAggregatesRequest,
) (*oraclev1.GetAggregatesResponse, error) {
	symbols := request.GetSymbols()
	if len(symbols) != 2 || symbols[0] != "BTC/USD" || symbols[1] != "ETH/USD" {
		return nil, fmt.Errorf("unexpected symbols: %v", symbols)
	}
	return &oraclev1.GetAggregatesResponse{Results: []*oraclev1.AggregatedResult{
		{Symbol: "BTC/USD", Value: "65000.250000000000000000", SourceCount: 3},
		{Symbol: "ETH/USD", Value: "3500.500000000000000000", SourceCount: 5},
	}}, nil
}

func main() {
	if len(os.Args) != 2 {
		panic("usage: pulsar-sidecar-server <socket>")
	}
	listener, err := net.Listen("unix", os.Args[1])
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	oraclev1.RegisterOracleSidecarServer(grpcServer, server{})
	if err := grpcServer.Serve(listener); err != nil {
		panic(err)
	}
}
