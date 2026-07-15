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
	"google.golang.org/protobuf/proto"
)

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
