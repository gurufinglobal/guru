package abci

import (
	"context"
	"math/big"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v2/config"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestMedianUsesOverflowSafeIntegerAtoms(t *testing.T) {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(sdkmath.LegacyPrecision), nil)
	maxAtoms := new(big.Int).Sub(
		new(big.Int).Mul(new(big.Int).Lsh(big.NewInt(1), 256), scale),
		big.NewInt(1),
	)
	maxValue := sdkmath.LegacyNewDecFromBigIntWithPrec(maxAtoms, sdkmath.LegacyPrecision)
	nearMaxValue := sdkmath.LegacyNewDecFromBigIntWithPrec(
		new(big.Int).Sub(new(big.Int).Set(maxAtoms), big.NewInt(1)),
		sdkmath.LegacyPrecision,
	)
	atto := sdkmath.LegacySmallestDec()

	tests := []struct {
		name   string
		values []sdkmath.LegacyDec
		want   sdkmath.LegacyDec
	}{
		{
			name: "odd sorts before selecting",
			values: []sdkmath.LegacyDec{
				sdkmath.LegacyMustNewDecFromStr("100"),
				sdkmath.LegacyMustNewDecFromStr("1"),
				sdkmath.LegacyMustNewDecFromStr("2"),
			},
			want: sdkmath.LegacyMustNewDecFromStr("2"),
		},
		{
			name: "even ordinary values",
			values: []sdkmath.LegacyDec{
				sdkmath.LegacyMustNewDecFromStr("2"),
				sdkmath.LegacyMustNewDecFromStr("1"),
			},
			want: sdkmath.LegacyMustNewDecFromStr("1.5"),
		},
		{
			name:   "positive half atto truncates toward zero",
			values: []sdkmath.LegacyDec{sdkmath.LegacyZeroDec(), atto},
			want:   sdkmath.LegacyZeroDec(),
		},
		{
			name:   "negative half atto truncates toward zero",
			values: []sdkmath.LegacyDec{atto.Neg(), sdkmath.LegacyZeroDec()},
			want:   sdkmath.LegacyZeroDec(),
		},
		{
			name:   "opposite full-range values",
			values: []sdkmath.LegacyDec{maxValue.Neg(), maxValue},
			want:   sdkmath.LegacyZeroDec(),
		},
		{
			name:   "full-range equal values avoid intermediate overflow",
			values: []sdkmath.LegacyDec{maxValue, maxValue},
			want:   maxValue,
		},
		{
			name:   "positive midpoint avoids intermediate overflow",
			values: []sdkmath.LegacyDec{nearMaxValue, maxValue},
			want:   nearMaxValue,
		},
		{
			name:   "negative midpoint avoids intermediate overflow",
			values: []sdkmath.LegacyDec{maxValue.Neg(), nearMaxValue.Neg()},
			want:   nearMaxValue.Neg(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := median(append([]sdkmath.LegacyDec(nil), tc.values...))
			require.True(t, got.Equal(tc.want), "got %s, want %s", got, tc.want)
		})
	}
}

func TestVerifyVoteExtensionAcceptsEmptyAndRejectsMalformed(t *testing.T) {
	handler := mustNewVoteExtensionHandler(t, nil, true, "", 0)

	empty, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{VoteExtension: nil})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, empty.Status)

	malformed, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{VoteExtension: []byte("bad")})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, malformed.Status)

	validBz, err := (&oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{
		Symbol:      "BTC/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	}}}).Marshal()
	require.NoError(t, err)
	valid, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{VoteExtension: validBz})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, valid.Status)
}

func TestExtendVoteReturnsEmptyWhenLocalOracleDisabled(t *testing.T) {
	handler := mustNewVoteExtensionHandler(t, nil, false, filepath.Join(t.TempDir(), "oracle.sock"), time.Second)

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

func TestAggregateValuesSkipsNonPositiveMinGasPriceTarget(t *testing.T) {
	voteA := mustVoteExtensionBz(t, appparams.MinGasPriceOracleSymbol, "0")
	voteB := mustVoteExtensionBz(t, appparams.MinGasPriceOracleSymbol, "-1.0")

	ctx := sdk.Context{}.WithBlockTime(time.Unix(99, 0))
	aggregator := Aggregator{keeper: fakeKeeper{
		params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
		tasks: []*oraclev1.OracleTask{{
			Symbol:             appparams.MinGasPriceOracleSymbol,
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
	require.Empty(t, values)
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

func TestOraclePayloadExpectedRequiresDueTasks(t *testing.T) {
	ctx := sdk.Context{}.
		WithBlockHeight(12).
		WithConsensusParams(cmtproto.ConsensusParams{Abci: &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 1}})

	aggregator := Aggregator{keeper: fakeKeeper{dueHeight: 99}}
	expected, err := aggregator.OraclePayloadExpected(ctx)
	require.NoError(t, err)
	require.False(t, expected)

	aggregator.keeper = fakeKeeper{
		tasks:     oracleTestTasks(),
		dueHeight: 11,
	}
	expected, err = aggregator.OraclePayloadExpected(ctx)
	require.NoError(t, err)
	require.True(t, expected)
}

func TestOraclePayloadExpectedVoteExtensionActivationBoundary(t *testing.T) {
	tests := []struct {
		name         string
		enableHeight int64
		blockHeight  int64
		dueTasks     bool
		wantExpected bool
		wantQueries  []int64
	}{
		{
			name:         "E=0 H=9 due tasks remain disabled",
			enableHeight: 0,
			blockHeight:  9,
			dueTasks:     true,
		},
		{
			name:         "E=0 H=9 no due tasks remain disabled",
			enableHeight: 0,
			blockHeight:  9,
		},
		{
			name:         "E=0 H=10 due tasks remain disabled",
			enableHeight: 0,
			blockHeight:  10,
			dueTasks:     true,
		},
		{
			name:         "E=0 H=10 no due tasks remain disabled",
			enableHeight: 0,
			blockHeight:  10,
		},
		{
			name:         "E=0 H=11 due tasks remain disabled",
			enableHeight: 0,
			blockHeight:  11,
			dueTasks:     true,
		},
		{
			name:         "E=0 H=11 no due tasks remain disabled",
			enableHeight: 0,
			blockHeight:  11,
		},
		{
			name:         "E=10 H=9 E-1 due tasks are not queried",
			enableHeight: 10,
			blockHeight:  9,
			dueTasks:     true,
		},
		{
			name:         "E=10 H=9 E-1 no due tasks are not queried",
			enableHeight: 10,
			blockHeight:  9,
		},
		{
			name:         "E=10 H=10 E due tasks are not queried",
			enableHeight: 10,
			blockHeight:  10,
			dueTasks:     true,
		},
		{
			name:         "E=10 H=10 E no due tasks are not queried",
			enableHeight: 10,
			blockHeight:  10,
		},
		{
			name:         "E=10 H=11 E+1 no due tasks queries E",
			enableHeight: 10,
			blockHeight:  11,
			wantQueries:  []int64{10},
		},
		{
			name:         "E=10 H=11 E+1 due tasks queries E",
			enableHeight: 10,
			blockHeight:  11,
			dueTasks:     true,
			wantExpected: true,
			wantQueries:  []int64{10},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keeper := &trackingKeeper{fakeKeeper: fakeKeeper{dueHeight: tc.blockHeight - 1}}
			if tc.dueTasks {
				keeper.tasks = oracleTestTasks()
			}

			ctx := sdk.Context{}.
				WithBlockHeight(tc.blockHeight).
				WithConsensusParams(cmtproto.ConsensusParams{Abci: &cmtproto.ABCIParams{
					VoteExtensionsEnableHeight: tc.enableHeight,
				}})

			expected, err := (Aggregator{keeper: keeper}).OraclePayloadExpected(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.wantExpected, expected)
			require.Equal(t, tc.wantQueries, keeper.dueTaskQueries)
		})
	}
}

func mustVoteExtensionBz(t *testing.T, symbol string, value string) []byte {
	t.Helper()

	canonicalValue := sdkmath.LegacyMustNewDecFromStr(value).String()
	bz, err := (&oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{
		Symbol:      symbol,
		Value:       canonicalValue,
		SourceCount: 3,
	}}}).Marshal()
	require.NoError(t, err)
	return bz
}

type fakeKeeper struct {
	params      *oraclev1.Params
	tasks       []*oraclev1.OracleTask
	dueHeight   int64
	paramsCalls *atomic.Int32
}

type trackingKeeper struct {
	fakeKeeper
	dueTaskQueries []int64
}

func (k *trackingKeeper) DueTasksForVoteExtension(ctx context.Context, height int64) ([]*oraclev1.OracleTask, error) {
	k.dueTaskQueries = append(k.dueTaskQueries, height)
	return k.fakeKeeper.DueTasksForVoteExtension(ctx, height)
}

func (f fakeKeeper) GetParams(context.Context) (*oraclev1.Params, error) {
	if f.paramsCalls != nil {
		f.paramsCalls.Add(1)
	}
	return f.params, nil
}

func (f fakeKeeper) DueTasksForVoteExtension(_ context.Context, height int64) ([]*oraclev1.OracleTask, error) {
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
