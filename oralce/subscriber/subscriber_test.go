package subscriber

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	oracletypes "github.com/GPTx-global/guru-v2/v2/x/oracle/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/assert"
	grpc "google.golang.org/grpc"
)

// mockQueryClient implements oracletypes.QueryClient for unit tests
type mockQueryClient struct {
	// configurable responses
	resDocs *oracletypes.QueryOracleRequestDocsResponse
	errDocs error

	resDoc *oracletypes.QueryOracleRequestDocResponse
	errDoc error
}

func (m *mockQueryClient) Params(ctx context.Context, in *oracletypes.QueryParamsRequest, opts ...grpc.CallOption) (*oracletypes.QueryParamsResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockQueryClient) OracleSubmitData(ctx context.Context, in *oracletypes.QueryOracleSubmitDataRequest, opts ...grpc.CallOption) (*oracletypes.QueryOracleSubmitDataResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockQueryClient) OracleData(ctx context.Context, in *oracletypes.QueryOracleDataRequest, opts ...grpc.CallOption) (*oracletypes.QueryOracleDataResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockQueryClient) OracleRequestDoc(ctx context.Context, in *oracletypes.QueryOracleRequestDocRequest, opts ...grpc.CallOption) (*oracletypes.QueryOracleRequestDocResponse, error) {
	if m != nil && m.resDoc != nil || m.errDoc != nil {
		return m.resDoc, m.errDoc
	}
	return &oracletypes.QueryOracleRequestDocResponse{}, nil
}

func (m *mockQueryClient) OracleRequestDocs(ctx context.Context, in *oracletypes.QueryOracleRequestDocsRequest, opts ...grpc.CallOption) (*oracletypes.QueryOracleRequestDocsResponse, error) {
	if m != nil && (m.resDocs != nil || m.errDocs != nil) {
		return m.resDocs, m.errDocs
	}
	return &oracletypes.QueryOracleRequestDocsResponse{}, nil
}

func (m *mockQueryClient) ModeratorAddress(ctx context.Context, in *oracletypes.QueryModeratorAddressRequest, opts ...grpc.CallOption) (*oracletypes.QueryModeratorAddressResponse, error) {
	return nil, errors.New("not implemented")
}

func TestParseRequestIDFromEvent(t *testing.T) {
	// 1) missing key
	{
		_, err := parseRequestIDFromEvent(coretypes.ResultEvent{}, "missing.key")
		assert.Error(t, err)
	}

	// 2) empty slice
	{
		event := coretypes.ResultEvent{Events: map[string][]string{"foo": {}}}
		_, err := parseRequestIDFromEvent(event, "foo")
		assert.Error(t, err)
	}

	// 3) non-numeric
	{
		event := coretypes.ResultEvent{Events: map[string][]string{"id": {"abc"}}}
		_, err := parseRequestIDFromEvent(event, "id")
		assert.Error(t, err)
	}

	// 4) valid zero
	{
		event := coretypes.ResultEvent{Events: map[string][]string{"id": {"0"}}}
		id, err := parseRequestIDFromEvent(event, "id")
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), id)
	}

	// 5) valid positive
	{
		event := coretypes.ResultEvent{Events: map[string][]string{"id": {"42"}}}
		id, err := parseRequestIDFromEvent(event, "id")
		assert.NoError(t, err)
		assert.Equal(t, uint64(42), id)
	}
}

func TestRunEventLoop_QueryDocsError(t *testing.T) {
	// Arrange
	s := &Subscriber{logger: log.NewNopLogger(), eventCh: make(chan any, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// mock returns error for OracleRequestDocs so loop exits immediately
	mqc := &mockQueryClient{resDocs: nil, errDocs: fmt.Errorf("boom")}

	regCh := make(chan coretypes.ResultEvent, 2)
	updCh := make(chan coretypes.ResultEvent, 2)
	compCh := make(chan coretypes.ResultEvent, 2)

	// Act
	go s.runEventLoop(ctx, mqc, regCh, updCh, compCh)

	// Assert first item is error
	select {
	case v := <-s.EventCh():
		assert.Error(t, v.(error))
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error event")
	}

	// Assert channel closed shortly after
	select {
	case _, ok := <-s.EventCh():
		assert.False(t, ok, "event channel should be closed")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for channel close")
	}
}
