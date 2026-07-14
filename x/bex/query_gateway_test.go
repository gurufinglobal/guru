package bex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/stretchr/testify/require"
	annotations "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestQuoteSwapRESTUsesQueryParameters(t *testing.T) {
	queryService := bexv1.File_guru_bex_v1_query_proto.Services().ByName("Query")
	require.NotNil(t, queryService)
	quoteMethod := queryService.Methods().ByName("QuoteSwap")
	require.NotNil(t, quoteMethod)

	options, ok := quoteMethod.Options().(*descriptorpb.MethodOptions)
	require.True(t, ok)
	require.True(t, proto.HasExtension(options, annotations.E_Http))
	httpRule, ok := proto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
	require.True(t, ok)
	require.Equal(t, "/guru/bex/v1/exchanges/{exchange_id}/quote", httpRule.GetGet())
}

func TestQuoteSwapRESTForwardsSlashDenom(t *testing.T) {
	capture := &quoteGatewayCapture{}
	mux := gatewayruntime.NewServeMux()
	require.NoError(t, bexv1.RegisterQueryHandlerServer(context.Background(), mux, capture))

	for _, denom := range []string{
		"ibc/0123456789ABCDEF",
		"factory/guru1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqe36g2/token",
	} {
		t.Run(denom, func(t *testing.T) {
			query := url.Values{
				"input_denom": {denom},
				"amount_in":   {"42"},
			}.Encode()
			request := httptest.NewRequest(
				http.MethodGet,
				"/guru/bex/v1/exchanges/7/quote?"+query,
				nil,
			)
			response := httptest.NewRecorder()

			mux.ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.NotNil(t, capture.lastRequest)
			require.Equal(t, uint64(7), capture.lastRequest.GetExchangeId())
			require.Equal(t, denom, capture.lastRequest.GetInputDenom())
			require.Equal(t, "42", capture.lastRequest.GetAmountIn())
		})
	}
}

type quoteGatewayCapture struct {
	bexv1.UnimplementedQueryServer
	lastRequest *bexv1.QueryQuoteSwapRequest
}

func (c *quoteGatewayCapture) QuoteSwap(
	_ context.Context,
	request *bexv1.QueryQuoteSwapRequest,
) (*bexv1.QueryQuoteSwapResponse, error) {
	c.lastRequest = proto.Clone(request).(*bexv1.QueryQuoteSwapRequest)
	return &bexv1.QueryQuoteSwapResponse{
		Quote: &bexv1.QuoteSwapResponse{
			ExchangeId: request.GetExchangeId(),
			InputDenom: request.GetInputDenom(),
			AmountIn:   request.GetAmountIn(),
		},
	}, nil
}
