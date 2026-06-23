package abci

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"cosmossdk.io/core/header"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	protoio "github.com/cometbft/cometbft/libs/protoio"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

const oracleTestChainID = "guru-oracle-test"

func TestPrepareProposalPrependsOneOraclePayloadAndStripsInjectedPayloads(t *testing.T) {
	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	ctx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), extCommit)
	aggregator := Aggregator{
		keeper: fakeKeeper{
			params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
			tasks:  oracleTestTasks(),
		},
		validatorStore: oracleValidatorStoreFor(validator),
	}

	injectedPayloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{Height: 99})
	require.NoError(t, err)
	normalA := []byte("normal-a")
	normalB := []byte("normal-b")
	prepareCalled := false
	handler := NewProposalHandler(
		aggregator,
		func(_ sdk.Context, req *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
			prepareCalled = true
			require.Equal(t, [][]byte{normalA, normalB}, req.Txs)
			return &abcitypes.ResponsePrepareProposal{
				Txs: [][]byte{normalB, injectedPayloadTx},
			}, nil
		},
		nil,
	)

	resp, err := handler.PrepareProposal(ctx, &abcitypes.RequestPrepareProposal{
		Height:          3,
		MaxTxBytes:      1_000_000,
		LocalLastCommit: extCommit,
		Txs:             [][]byte{normalA, injectedPayloadTx, normalB},
	})
	require.NoError(t, err)
	require.True(t, prepareCalled)
	require.Len(t, resp.Txs, 2)
	require.True(t, IsProposalTx(resp.Txs[0]))
	require.False(t, IsProposalTx(resp.Txs[1]))
	require.Equal(t, normalB, resp.Txs[1])

	payload, hasPayload, err := DecodeProposalTx(resp.Txs[0])
	require.NoError(t, err)
	require.True(t, hasPayload)
	require.Equal(t, int64(3), payload.GetHeight())
	require.Len(t, payload.GetValues(), 1)
	require.Equal(t, "BTC/USD", payload.GetValues()[0].GetSymbol())
}

func TestPrepareProposalDoesNotIncludeNormalTxWhenPayloadUsesMaxTxBytes(t *testing.T) {
	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	ctx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), extCommit)
	aggregator := Aggregator{
		keeper: fakeKeeper{
			params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
			tasks:  oracleTestTasks(),
		},
		validatorStore: oracleValidatorStoreFor(validator),
	}
	payload, err := aggregator.BuildPayload(ctx, 3, extCommit)
	require.NoError(t, err)
	payloadTx, err := EncodeProposalTx(payload)
	require.NoError(t, err)
	maxTxBytes := proposalTxBytes([][]byte{payloadTx})

	normalTx := []byte("normal")
	var innerMaxTxBytes int64
	handler := NewProposalHandler(
		aggregator,
		func(_ sdk.Context, req *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
			innerMaxTxBytes = req.MaxTxBytes
			return &abcitypes.ResponsePrepareProposal{Txs: [][]byte{normalTx}}, nil
		},
		nil,
	)

	resp, err := handler.PrepareProposal(ctx, &abcitypes.RequestPrepareProposal{
		Height:          3,
		MaxTxBytes:      maxTxBytes,
		LocalLastCommit: extCommit,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), innerMaxTxBytes)
	require.Equal(t, [][]byte{payloadTx}, resp.Txs)
	require.LessOrEqual(t, proposalTxBytes(resp.Txs), maxTxBytes)
}

func TestPrepareProposalTrimsNormalTxsToRemainingMaxTxBytes(t *testing.T) {
	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	ctx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), extCommit)
	aggregator := Aggregator{
		keeper: fakeKeeper{
			params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
			tasks:  oracleTestTasks(),
		},
		validatorStore: oracleValidatorStoreFor(validator),
	}
	payload, err := aggregator.BuildPayload(ctx, 3, extCommit)
	require.NoError(t, err)
	payloadTx, err := EncodeProposalTx(payload)
	require.NoError(t, err)

	normalA := []byte("a")
	normalB := []byte("normal-b-that-does-not-fit")
	payloadBytes := proposalTxBytes([][]byte{payloadTx})
	normalABytes := proposalTxBytes([][]byte{normalA})
	maxTxBytes := payloadBytes + normalABytes

	var innerMaxTxBytes int64
	handler := NewProposalHandler(
		aggregator,
		func(_ sdk.Context, req *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
			innerMaxTxBytes = req.MaxTxBytes
			return &abcitypes.ResponsePrepareProposal{Txs: [][]byte{normalA, normalB}}, nil
		},
		nil,
	)

	resp, err := handler.PrepareProposal(ctx, &abcitypes.RequestPrepareProposal{
		Height:          3,
		MaxTxBytes:      maxTxBytes,
		LocalLastCommit: extCommit,
	})
	require.NoError(t, err)
	require.Equal(t, normalABytes, innerMaxTxBytes)
	require.Equal(t, [][]byte{payloadTx, normalA}, resp.Txs)
	require.LessOrEqual(t, proposalTxBytes(resp.Txs), maxTxBytes)
}

func TestProcessProposalAcceptsRecomputedPayloadAndRejectsMismatch(t *testing.T) {
	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	ctx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), extCommit)
	aggregator := Aggregator{
		keeper: fakeKeeper{
			params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
			tasks:  oracleTestTasks(),
		},
		validatorStore: oracleValidatorStoreFor(validator),
	}

	payload, err := aggregator.BuildPayload(ctx, 3, extCommit)
	require.NoError(t, err)
	payloadTx, err := EncodeProposalTx(payload)
	require.NoError(t, err)

	normalTx := []byte("normal")
	processCalled := false
	handler := NewProposalHandler(
		aggregator,
		nil,
		func(_ sdk.Context, req *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
			processCalled = true
			require.Equal(t, [][]byte{normalTx}, req.Txs)
			return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
		},
	)

	resp, err := handler.ProcessProposal(ctx, &abcitypes.RequestProcessProposal{
		Txs: [][]byte{payloadTx, normalTx},
	})
	require.NoError(t, err)
	require.True(t, processCalled)
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, resp.Status)

	mismatchedPayload := *payload
	mismatchedPayload.Values = []*oraclev1.OracleValue{{
		Symbol:        "BTC/USD",
		ValueType:     oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:         "9.0",
		BlockHeight:   3,
		BlockTimeUnix: 30,
	}}
	mismatchedPayloadTx, err := EncodeProposalTx(&mismatchedPayload)
	require.NoError(t, err)

	processCalled = false
	mismatchedResp, err := handler.ProcessProposal(ctx, &abcitypes.RequestProcessProposal{
		Txs: [][]byte{mismatchedPayloadTx, normalTx},
	})
	require.NoError(t, err)
	require.False(t, processCalled)
	require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, mismatchedResp.Status)
}

func TestProcessProposalRejectsPayloadWithMutatedBlockIDFlag(t *testing.T) {
	validators := []oracleTestValidator{
		newOracleTestValidator(),
		newOracleTestValidator(),
		newOracleTestValidator(),
		newOracleTestValidator(),
	}
	extCommit := signedOracleExtCommitForValues(t, 3, validators, []string{"1.0", "2.0", "100.0", "100.0"})
	ctx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), extCommit)
	aggregator := Aggregator{
		keeper: fakeKeeper{
			params: &oraclev1.Params{MinValidators: 3, MinSources: 3, HistoryLimit: 100},
			tasks:  oracleTestTasks(),
		},
		validatorStore: oracleValidatorStoreFor(validators...),
	}

	honestPayload, err := aggregator.BuildPayload(ctx, 3, extCommit)
	require.NoError(t, err)
	require.Len(t, honestPayload.GetValues(), 1)

	mutatedCommit := cloneExtendedCommit(extCommit)
	mutatedCommit.Votes[0].BlockIdFlag = cmtproto.BlockIDFlagNil
	mutatedValues, err := aggregator.aggregateValues(ctx, 3, mutatedCommit)
	require.NoError(t, err)
	require.Len(t, mutatedValues, 1)
	require.NotEqual(t, honestPayload.GetValues()[0].GetValue(), mutatedValues[0].GetValue())

	mutatedPayloadTx, err := EncodeProposalTx(&oraclev1.OracleProposalPayload{
		Height:         3,
		VoteExtensions: signedVoteExtensionsFromExtendedCommit(mutatedCommit),
		Values:         mutatedValues,
	})
	require.NoError(t, err)

	processCalled := false
	handler := NewProposalHandler(
		aggregator,
		nil,
		func(sdk.Context, *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
			processCalled = true
			return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
		},
	)

	resp, err := handler.ProcessProposal(ctx, &abcitypes.RequestProcessProposal{
		Txs: [][]byte{mutatedPayloadTx},
	})
	require.NoError(t, err)
	require.False(t, processCalled)
	require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, resp.Status)
}

func TestApplyProposalPayloadPersistsLatestValueAndBoundedHistory(t *testing.T) {
	baseCtx, keeper := setupOracleABCIKeeper(t, &oraclev1.Params{
		MinValidators: 1,
		MinSources:    3,
		HistoryLimit:  2,
	})
	require.NoError(t, keeper.SetTask(baseCtx, oracleTestTasks()[0]))
	require.NoError(t, keeper.AdvanceTaskSchedule(baseCtx, 1))
	require.NoError(t, keeper.ApplyOracleValues(baseCtx, []*oraclev1.OracleValue{oracleValue("BTC/USD", "0.5", 2, 20)}))

	validator := newOracleTestValidator()
	aggregator := NewAggregator(keeper, oracleValidatorStoreFor(validator))
	handler := NewProposalHandler(aggregator, nil, nil)

	for _, tc := range []struct {
		height int64
		value  string
	}{
		{height: 3, value: "1.0"},
		{height: 4, value: "2.0"},
	} {
		extCommit := signedOracleExtCommit(t, tc.height, validator, tc.value)
		ctx := withOracleProposalContext(baseCtx, tc.height, time.Unix(tc.height*10, 0), extCommit)
		payload, err := aggregator.BuildPayload(ctx, tc.height, extCommit)
		require.NoError(t, err)
		payloadTx, err := EncodeProposalTx(payload)
		require.NoError(t, err)
		require.NoError(t, handler.ApplyProposalPayload(ctx, &abcitypes.RequestFinalizeBlock{
			Txs: [][]byte{payloadTx},
		}))
	}

	latest, err := keeper.GetLatestValue(baseCtx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, "2.000000000000000000", latest.GetValue())
	require.Equal(t, int64(4), latest.GetBlockHeight())
	require.Equal(t, int64(40), latest.GetBlockTimeUnix())

	history, err := keeper.GetHistory(baseCtx, "BTC/USD")
	require.NoError(t, err)
	require.Len(t, history.GetValues(), 2)
	require.Equal(t, int64(3), history.GetValues()[0].GetBlockHeight())
	require.Equal(t, int64(4), history.GetValues()[1].GetBlockHeight())
}

func TestQuorumFailureLeavesLatestValueUnchanged(t *testing.T) {
	baseCtx, keeper := setupOracleABCIKeeper(t, &oraclev1.Params{
		MinValidators: 2,
		MinSources:    3,
		HistoryLimit:  10,
	})
	require.NoError(t, keeper.SetTask(baseCtx, oracleTestTasks()[0]))
	require.NoError(t, keeper.AdvanceTaskSchedule(baseCtx, 1))
	require.NoError(t, keeper.ApplyOracleValues(baseCtx, []*oraclev1.OracleValue{oracleValue("BTC/USD", "10.0", 2, 20)}))

	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	ctx := withOracleProposalContext(baseCtx, 3, time.Unix(30, 0), extCommit)
	aggregator := NewAggregator(keeper, oracleValidatorStoreFor(validator))
	payload, err := aggregator.BuildPayload(ctx, 3, extCommit)
	require.NoError(t, err)
	require.Empty(t, payload.GetValues())
	payloadTx, err := EncodeProposalTx(payload)
	require.NoError(t, err)

	handler := NewProposalHandler(aggregator, nil, nil)
	require.NoError(t, handler.ApplyProposalPayload(ctx, &abcitypes.RequestFinalizeBlock{
		Txs: [][]byte{payloadTx},
	}))

	latest, err := keeper.GetLatestValue(baseCtx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, "10.0", latest.GetValue())
	require.Equal(t, int64(2), latest.GetBlockHeight())
}

func TestQuorumFailureAdvancesIntervalScheduleWithoutEveryBlockRetry(t *testing.T) {
	baseCtx, keeper := setupOracleABCIKeeper(t, &oraclev1.Params{
		MinValidators: 2,
		MinSources:    3,
		HistoryLimit:  10,
	})
	require.NoError(t, keeper.SetTask(baseCtx.WithBlockHeight(0), &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 5,
	}))
	require.NoError(t, keeper.ApplyOracleValues(baseCtx, []*oraclev1.OracleValue{oracleValue("BTC/USD", "10.0", 4, 40)}))

	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 6, validator, "1.0")
	ctx := withOracleProposalContext(baseCtx, 6, time.Unix(60, 0), extCommit)
	aggregator := NewAggregator(keeper, oracleValidatorStoreFor(validator))
	payload, err := aggregator.BuildPayload(ctx, 6, extCommit)
	require.NoError(t, err)
	require.Empty(t, payload.GetValues())
	payloadTx, err := EncodeProposalTx(payload)
	require.NoError(t, err)

	handler := NewProposalHandler(aggregator, nil, nil)
	require.NoError(t, handler.ApplyProposalPayload(ctx, &abcitypes.RequestFinalizeBlock{
		Txs: [][]byte{payloadTx},
	}))

	latest, err := keeper.GetLatestValue(baseCtx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, "10.0", latest.GetValue())

	for _, height := range []int64{6, 7, 8, 9} {
		due, err := keeper.DueTasksForVoteExtension(baseCtx, height)
		require.NoError(t, err)
		require.Empty(t, due)
	}
	due, err := keeper.DueTasksForVoteExtension(baseCtx, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, "BTC/USD", due[0].GetSymbol())
}

func setupOracleABCIKeeper(t *testing.T, params *oraclev1.Params) (sdk.Context, oraclekeeper.Keeper) {
	t.Helper()

	key := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_oracle_abci_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)
	keeper := oraclekeeper.NewKeeper(
		runtime.NewKVStoreService(key),
		evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr),
		abciConstitutionKeeper{},
	)
	require.NoError(t, keeper.SetParams(testCtx.Ctx, params))

	return testCtx.Ctx, keeper
}

type abciConstitutionKeeper struct{}

func (abciConstitutionKeeper) GetModeratorAddress(context.Context) (string, error) {
	return "", nil
}

func oracleTestTasks() []*oraclev1.OracleTask {
	return []*oraclev1.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}}
}

func oracleValue(symbol string, value string, height int64, blockTimeUnix int64) *oraclev1.OracleValue {
	return &oraclev1.OracleValue{
		Symbol:        symbol,
		ValueType:     oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:         value,
		BlockHeight:   height,
		BlockTimeUnix: blockTimeUnix,
	}
}

func withOracleProposalContext(ctx sdk.Context, height int64, blockTime time.Time, extCommit abcitypes.ExtendedCommitInfo) sdk.Context {
	lastCommit := abcitypes.CommitInfo{
		Round: extCommit.Round,
		Votes: make([]abcitypes.VoteInfo, len(extCommit.GetVotes())),
	}
	for i, vote := range extCommit.GetVotes() {
		lastCommit.Votes[i] = abcitypes.VoteInfo{
			Validator: abcitypes.Validator{
				Address: append([]byte(nil), vote.Validator.Address...),
				Power:   vote.Validator.Power,
			},
			BlockIdFlag: vote.BlockIdFlag,
		}
	}

	return ctx.
		WithBlockHeight(height).
		WithBlockTime(blockTime).
		WithChainID(oracleTestChainID).
		WithHeaderInfo(header.Info{Height: height, ChainID: oracleTestChainID, Time: blockTime}).
		WithConsensusParams(cmtproto.ConsensusParams{Abci: &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 1}}).
		WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, lastCommit))
}

type oracleTestValidator struct {
	consAddr sdk.ConsAddress
	pubKey   cmtprotocrypto.PublicKey
	privKey  cmtsecp256k1.PrivKey
}

func newOracleTestValidator() oracleTestValidator {
	privKey := cmtsecp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	return oracleTestValidator{
		consAddr: sdk.ConsAddress(pubKey.Address()),
		pubKey: cmtprotocrypto.PublicKey{
			Sum: &cmtprotocrypto.PublicKey_Secp256K1{
				Secp256K1: pubKey.Bytes(),
			},
		},
		privKey: privKey,
	}
}

func signedOracleExtCommit(t *testing.T, height int64, validator oracleTestValidator, value string) abcitypes.ExtendedCommitInfo {
	t.Helper()

	return signedOracleExtCommitForValues(t, height, []oracleTestValidator{validator}, []string{value})
}

func signedOracleExtCommitForValues(t *testing.T, height int64, validators []oracleTestValidator, values []string) abcitypes.ExtendedCommitInfo {
	t.Helper()

	require.Len(t, values, len(validators))
	votes := make([]abcitypes.ExtendedVoteInfo, 0, len(validators))
	for i, validator := range validators {
		votes = append(votes, signedOracleVoteInfo(t, height, validator, values[i]))
	}
	sort.Slice(votes, func(i, j int) bool {
		if votes[i].Validator.Power == votes[j].Validator.Power {
			return bytes.Compare(votes[i].Validator.Address, votes[j].Validator.Address) < 0
		}
		return votes[i].Validator.Power > votes[j].Validator.Power
	})

	return abcitypes.ExtendedCommitInfo{
		Round: 0,
		Votes: votes,
	}
}

func signedOracleVoteInfo(t *testing.T, height int64, validator oracleTestValidator, value string) abcitypes.ExtendedVoteInfo {
	t.Helper()

	voteExtension := mustVoteExtensionBz(t, "BTC/USD", value)
	signBytes := bytes.Buffer{}
	_, err := protoio.NewDelimitedWriter(&signBytes).WriteMsg(&cmtproto.CanonicalVoteExtension{
		Extension: voteExtension,
		Height:    height - 1,
		Round:     0,
		ChainId:   oracleTestChainID,
	})
	require.NoError(t, err)
	signature, err := validator.privKey.Sign(signBytes.Bytes())
	require.NoError(t, err)

	return abcitypes.ExtendedVoteInfo{
		Validator: abcitypes.Validator{
			Address: validator.consAddr.Bytes(),
			Power:   1,
		},
		BlockIdFlag:        cmtproto.BlockIDFlagCommit,
		VoteExtension:      voteExtension,
		ExtensionSignature: signature,
	}
}

func cloneExtendedCommit(extCommit abcitypes.ExtendedCommitInfo) abcitypes.ExtendedCommitInfo {
	votes := make([]abcitypes.ExtendedVoteInfo, 0, len(extCommit.GetVotes()))
	for _, vote := range extCommit.GetVotes() {
		votes = append(votes, abcitypes.ExtendedVoteInfo{
			Validator: abcitypes.Validator{
				Address: append([]byte(nil), vote.Validator.Address...),
				Power:   vote.Validator.Power,
			},
			BlockIdFlag:        vote.BlockIdFlag,
			VoteExtension:      append([]byte(nil), vote.VoteExtension...),
			ExtensionSignature: append([]byte(nil), vote.ExtensionSignature...),
		})
	}

	return abcitypes.ExtendedCommitInfo{
		Round: extCommit.Round,
		Votes: votes,
	}
}

type oracleValidatorStore map[string]cmtprotocrypto.PublicKey

func oracleValidatorStoreFor(validators ...oracleTestValidator) oracleValidatorStore {
	store := make(oracleValidatorStore, len(validators))
	for _, validator := range validators {
		store[string(validator.consAddr.Bytes())] = validator.pubKey
	}
	return store
}

func (s oracleValidatorStore) GetPubKeyByConsAddr(_ context.Context, addr sdk.ConsAddress) (cmtprotocrypto.PublicKey, error) {
	pubKey, ok := s[string(addr)]
	if !ok {
		return cmtprotocrypto.PublicKey{}, fmt.Errorf("unknown validator %X", addr)
	}
	return pubKey, nil
}
