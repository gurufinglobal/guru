package main

import (
	"context"
	"fmt"
	"os"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: gogo-node-client <socket>")
	}
	connection, err := grpc.NewClient(
		"unix://"+os.Args[1],
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(err)
	}
	defer connection.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := oraclev1.NewOracleSidecarClient(connection).GetAggregates(
		ctx,
		&oraclev1.GetAggregatesRequest{Symbols: []string{"BTC/USD", "ETH/USD"}},
	)
	if err != nil {
		panic(err)
	}
	results := response.GetResults()
	if len(results) != 2 {
		panic(fmt.Sprintf("unexpected result count: %d", len(results)))
	}
	if got := results[0]; got.GetSymbol() != "BTC/USD" ||
		got.GetValue() != "65000.250000000000000000" ||
		got.GetSourceCount() != 3 {
		panic(fmt.Sprintf("unexpected first result: %+v", got))
	}
	if got := results[1]; got.GetSymbol() != "ETH/USD" ||
		got.GetValue() != "3500.500000000000000000" ||
		got.GetSourceCount() != 5 {
		panic(fmt.Sprintf("unexpected second result: %+v", got))
	}
}
