package abci

import (
	"context"
	"testing"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestValidatorResultsFromSamplesComputesMedian(t *testing.T) {
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
				{Source: "c", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Value: "100.0"},
			},
		}},
		3,
	)

	require.Len(t, results, 1)
	require.Equal(t, "BTC/USD", results[0].GetSymbol())
	require.Equal(t, "2.000000000000000000", results[0].GetValue())
	require.Equal(t, uint32(3), results[0].GetSourceCount())
}

func TestValidatorResultsFromSamplesAveragesEvenMedian(t *testing.T) {
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
		2,
	)

	require.Len(t, results, 1)
	require.Equal(t, "1.500000000000000000", results[0].GetValue())
}

func TestValidatorResultsFromSamplesNormalizesMixedCaseSymbols(t *testing.T) {
	results := validatorResultsFromSamples(
		[]*oraclev1.OracleTask{{
			Symbol:             " btc/usd ",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		}},
		[]*oraclev1.OracleSymbolSamples{{
			Symbol: "BTC/USD",
			Samples: []*oraclev1.OracleSample{
				{Source: "a", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Value: "1.0"},
				{Source: "b", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Value: "2.0"},
				{Source: "c", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Value: "3.0"},
			},
		}},
		3,
	)

	require.Len(t, results, 1)
	require.Equal(t, "BTC/USD", results[0].GetSymbol())
	require.Equal(t, "2.000000000000000000", results[0].GetValue())
}

func TestVerifyVoteExtensionAcceptsEmptyAndRejectsMalformed(t *testing.T) {
	handler := NewVoteExtensionHandler(nil, true, "", 0)

	empty, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{VoteExtension: nil})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, empty.Status)

	malformed, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{VoteExtension: []byte("bad")})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, malformed.Status)

	validBz, err := proto.Marshal(&oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{
		Symbol:      "BTC/USD",
		ValueType:   oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:       "1.0",
		SourceCount: 3,
	}}})
	require.NoError(t, err)
	valid, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{VoteExtension: validBz})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, valid.Status)
}

func TestExtendVoteReturnsEmptyWhenLocalOracleDisabled(t *testing.T) {
	handler := NewVoteExtensionHandler(nil, false, "/tmp/oracle.sock", time.Second)

	resp, err := handler.ExtendVote(sdk.Context{}, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, resp.VoteExtension)
}

func TestAggregateValuesUsesUnweightedMedianOfValidatorMedians(t *testing.T) {
	voteA := mustVoteExtensionBz(t, "BTC/USD", "1.0")
	voteB := mustVoteExtensionBz(t, "BTC/USD", "2.0")

	ctx := sdk.Context{}.WithBlockTime(time.Unix(99, 0))
	aggregator := Aggregator{keeper: fakeKeeper{
		params: &oraclev1.Params{MinValidators: 2, MinSources: 3, HistoryLimit: 100},
		tasks: []*oraclev1.OracleTask{{
			Symbol:             "BTC/USD",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		}},
	}}

	values, err := aggregator.aggregateValues(ctx, 12, abcitypes.ExtendedCommitInfo{
		Votes: []abcitypes.ExtendedVoteInfo{
			{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: voteA},
			{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: voteB},
		},
	})
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Equal(t, "1.500000000000000000", values[0].GetValue())
	require.Equal(t, int64(12), values[0].GetBlockHeight())
	require.Equal(t, int64(99), values[0].GetBlockTimeUnix())
}

func TestAggregateValuesUsesOnlyDueTasksForPreviousHeight(t *testing.T) {
	vote := mustVoteExtensionBz(t, "BTC/USD", "1.0")

	ctx := sdk.Context{}.WithBlockTime(time.Unix(99, 0))
	aggregator := Aggregator{keeper: fakeKeeper{
		params:    &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
		tasks:     oracleTestTasks(),
		dueHeight: 99,
	}}

	values, err := aggregator.aggregateValues(ctx, 12, abcitypes.ExtendedCommitInfo{
		Votes: []abcitypes.ExtendedVoteInfo{
			{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: vote},
		},
	})
	require.NoError(t, err)
	require.Empty(t, values)

	aggregator.keeper = fakeKeeper{
		params:    &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
		tasks:     oracleTestTasks(),
		dueHeight: 11,
	}
	values, err = aggregator.aggregateValues(ctx, 12, abcitypes.ExtendedCommitInfo{
		Votes: []abcitypes.ExtendedVoteInfo{
			{BlockIdFlag: cmtproto.BlockIDFlagCommit, VoteExtension: vote},
		},
	})
	require.NoError(t, err)
	require.Len(t, values, 1)
}

func mustVoteExtensionBz(t *testing.T, symbol string, value string) []byte {
	t.Helper()

	bz, err := proto.Marshal(&oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{
		Symbol:      symbol,
		ValueType:   oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:       value,
		SourceCount: 3,
	}}})
	require.NoError(t, err)
	return bz
}

type fakeKeeper struct {
	params    *oraclev1.Params
	tasks     []*oraclev1.OracleTask
	dueHeight int64
}

func (f fakeKeeper) GetParams(context.Context) (*oraclev1.Params, error) {
	return f.params, nil
}

func (f fakeKeeper) DueTasks(_ context.Context, height int64) ([]*oraclev1.OracleTask, error) {
	if f.dueHeight != 0 && f.dueHeight != height {
		return nil, nil
	}

	return f.tasks, nil
}

func (f fakeKeeper) AdvanceTaskSchedule(context.Context, int64) error {
	return nil
}

func (f fakeKeeper) ApplyOracleValues(context.Context, []*oraclev1.OracleValue) error {
	return nil
}
