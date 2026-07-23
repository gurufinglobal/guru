package abci

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidatorResultsFromSamplesSkipsFewerThanMinSources(t *testing.T) {
	results := validatorResultsFromSamples(
		[]*oraclev1.OracleTask{{
			Symbol:             "BTC/USD",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		}},
		[]*oraclev1.OracleSymbolSamples{{
			Symbol: "BTC/USD",
			Samples: []*oraclev1.OracleSample{
				{Source: "a", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Value: "1.0"},
				{Source: "b", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Value: "2.0"},
			},
		}},
		3,
	)

	require.Empty(t, results)
}

func TestVerifyVoteExtensionRejectsDuplicateSymbolAndInvalidNumeric(t *testing.T) {
	handler := NewVoteExtensionHandler(nil, true, "", 0)

	duplicateBz, err := (&oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{
		{
			Symbol:      "BTC/USD",
			ValueType:   oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Value:       "1.0",
			SourceCount: 3,
		},
		{
			Symbol:      " btc/usd ",
			ValueType:   oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Value:       "2.0",
			SourceCount: 3,
		},
	}}).Marshal()
	require.NoError(t, err)
	duplicateResp, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{
		VoteExtension: duplicateBz,
	})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, duplicateResp.Status)

	invalidNumericBz, err := (&oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{
		Symbol:      "BTC/USD",
		ValueType:   oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:       "not-a-number",
		SourceCount: 3,
	}}}).Marshal()
	require.NoError(t, err)
	invalidNumericResp, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{
		VoteExtension: invalidNumericBz,
	})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, invalidNumericResp.Status)

	nonNumericBz, err := (&oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{
		Symbol:      "BTC/USD",
		ValueType:   oraclev1.ValueType_VALUE_TYPE_STRING,
		Value:       "up",
		SourceCount: 3,
	}}}).Marshal()
	require.NoError(t, err)
	nonNumericResp, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{
		VoteExtension: nonNumericBz,
	})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, nonNumericResp.Status)
}

func TestExtendVoteReturnsEmptyOnSidecarFailures(t *testing.T) {
	keeper := fakeKeeper{
		params: &oraclev1.Params{MinValidators: 1, MinSources: 1, HistoryLimit: 100},
		tasks: []*oraclev1.OracleTask{{
			Symbol:             "BTC/USD",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		}},
	}
	ctx := sdk.Context{}.WithContext(context.Background())

	unsetSocketHandler := NewVoteExtensionHandler(keeper, true, "", time.Millisecond)
	unsetSocketResp, err := unsetSocketHandler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, unsetSocketResp.VoteExtension)

	timeoutSocket := startTestSidecar(t, testSidecar{delay: 50 * time.Millisecond})
	timeoutHandler := NewVoteExtensionHandler(keeper, true, timeoutSocket, time.Millisecond)
	timeoutResp, err := timeoutHandler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, timeoutResp.VoteExtension)

	errorSocket := startTestSidecar(t, testSidecar{err: errors.New("daemon error")})
	errorHandler := NewVoteExtensionHandler(keeper, true, errorSocket, time.Second)
	errorResp, err := errorHandler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, errorResp.VoteExtension)
}

func startTestSidecar(t *testing.T, sidecar testSidecar) string {
	t.Helper()

	socketDir, err := os.MkdirTemp("/tmp", "guru-oracle-")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(socketDir))
	})

	socket := filepath.Join(socketDir, "oracle.sock")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)

	server := grpc.NewServer()
	oraclev1.RegisterOracleSidecarServer(server, sidecar)
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Logf("oracle sidecar test server stopped: %v", err)
		}
	}()

	return socket
}

type testSidecar struct {
	oraclev1.UnimplementedOracleSidecarServer

	delay time.Duration
	err   error
}

func (s testSidecar) GetSamples(ctx context.Context, _ *oraclev1.GetSamplesRequest) (*oraclev1.GetSamplesResponse, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, status.Error(codes.Unavailable, s.err.Error())
	}

	return &oraclev1.GetSamplesResponse{Symbols: []*oraclev1.OracleSymbolSamples{{
		Symbol: "BTC/USD",
		Samples: []*oraclev1.OracleSample{{
			Source:    "a",
			ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Value:     "1.0",
		}},
	}}}, nil
}
