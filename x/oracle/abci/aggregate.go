package abci

import (
	"context"
	"fmt"
	"sort"

	sdkmath "cosmossdk.io/math"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	"google.golang.org/protobuf/proto"
)

type Keeper interface {
	GetParams(ctx context.Context) (*oraclev1.Params, error)
	DueTasksForVoteExtension(ctx context.Context, height int64) ([]*oraclev1.OracleTask, error)
	AdvanceTaskSchedule(ctx context.Context, height int64) error
	ApplyOracleValues(ctx context.Context, values []*oraclev1.OracleValue) error
}

type Aggregator struct {
	keeper         Keeper
	validatorStore baseapp.ValidatorStore
}

func NewAggregator(keeper Keeper, validatorStore baseapp.ValidatorStore) Aggregator {
	return Aggregator{
		keeper:         keeper,
		validatorStore: validatorStore,
	}
}

func (a Aggregator) OraclePayloadExpected(ctx sdk.Context) (bool, error) {
	abciParams := ctx.ConsensusParams().Abci
	if abciParams == nil ||
		abciParams.VoteExtensionsEnableHeight == 0 ||
		ctx.BlockHeight() <= abciParams.VoteExtensionsEnableHeight {
		return false, nil
	}

	tasks, err := a.keeper.DueTasksForVoteExtension(ctx, voteExtensionHeight(ctx.BlockHeight()))
	if err != nil {
		return false, err
	}

	return len(tasks) != 0, nil
}

func (a Aggregator) BuildPayload(ctx sdk.Context, height int64, extCommit abcitypes.ExtendedCommitInfo) (*oraclev1.OracleProposalPayload, error) {
	expected, err := a.OraclePayloadExpected(ctx)
	if err != nil {
		return nil, err
	}
	if !expected {
		return nil, nil
	}

	if err := baseapp.ValidateVoteExtensions(ctx, a.validatorStore, 0, "", extCommit); err != nil {
		return nil, err
	}
	if err := validateExtendedCommitBlockIDFlags(ctx, extCommit); err != nil {
		return nil, err
	}

	values, err := a.aggregateValues(ctx, height, extCommit)
	if err != nil {
		return nil, err
	}

	return &oraclev1.OracleProposalPayload{
		Height:         height,
		VoteExtensions: signedVoteExtensionsFromExtendedCommit(extCommit),
		Values:         values,
	}, nil
}

func (a Aggregator) VerifyPayload(ctx sdk.Context, payload *oraclev1.OracleProposalPayload) error {
	expected, err := a.OraclePayloadExpected(ctx)
	if err != nil {
		return err
	}
	if !expected {
		if payload != nil {
			return fmt.Errorf("oracle payload is not expected at height %d", ctx.BlockHeight())
		}
		return nil
	}
	if payload == nil {
		return fmt.Errorf("missing oracle payload at height %d", ctx.BlockHeight())
	}
	if payload.GetHeight() != ctx.BlockHeight() {
		return fmt.Errorf("oracle payload height %d does not match block height %d", payload.GetHeight(), ctx.BlockHeight())
	}

	extCommit := extendedCommitFromSignedVoteExtensions(payload.GetVoteExtensions())
	expectedPayload, err := a.BuildPayload(ctx, ctx.BlockHeight(), extCommit)
	if err != nil {
		return err
	}
	if expectedPayload == nil {
		return fmt.Errorf("oracle payload unexpectedly disabled at height %d", ctx.BlockHeight())
	}
	if !oracleValuesEqual(payload.GetValues(), expectedPayload.GetValues()) {
		return fmt.Errorf("oracle payload values do not match recomputed values")
	}

	return nil
}

func (a Aggregator) ApplyPayload(ctx sdk.Context, payload *oraclev1.OracleProposalPayload) error {
	if err := a.VerifyPayload(ctx, payload); err != nil {
		return err
	}
	if payload == nil {
		return nil
	}
	if len(payload.GetValues()) != 0 {
		if err := a.keeper.ApplyOracleValues(ctx, payload.GetValues()); err != nil {
			return err
		}
	}

	return a.keeper.AdvanceTaskSchedule(ctx, voteExtensionHeight(payload.GetHeight()))
}

func (a Aggregator) aggregateValues(ctx sdk.Context, height int64, extCommit abcitypes.ExtendedCommitInfo) ([]*oraclev1.OracleValue, error) {
	params, err := a.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	tasks, err := a.keeper.DueTasksForVoteExtension(ctx, voteExtensionHeight(height))
	if err != nil {
		return nil, err
	}

	taskBySymbol := make(map[string]*oraclev1.OracleTask, len(tasks))
	for _, task := range tasks {
		taskBySymbol[oraclekeeper.NormalizeSymbol(task.GetSymbol())] = task
	}

	valuesBySymbol := make(map[string][]sdkmath.LegacyDec, len(taskBySymbol))
	for _, vote := range extCommit.GetVotes() {
		if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit || len(vote.VoteExtension) == 0 {
			continue
		}

		extension, err := DecodeVoteExtension(vote.VoteExtension)
		if err != nil {
			return nil, err
		}
		for _, result := range extension.GetResults() {
			symbol := oraclekeeper.NormalizeSymbol(result.GetSymbol())
			task, ok := taskBySymbol[symbol]
			if !ok || task.GetValueType() != result.GetValueType() {
				continue
			}
			if result.GetSourceCount() < params.GetMinSources() {
				continue
			}

			value, err := sdkmath.LegacyNewDecFromStr(result.GetValue())
			if err != nil {
				return nil, err
			}
			if isMinGasPriceOracleSymbol(symbol) && !value.IsPositive() {
				continue
			}
			valuesBySymbol[symbol] = append(valuesBySymbol[symbol], value)
		}
	}

	symbols := make([]string, 0, len(valuesBySymbol))
	for symbol, values := range valuesBySymbol {
		if uint32(len(values)) >= params.GetMinValidators() {
			symbols = append(symbols, symbol)
		}
	}
	sort.Strings(symbols)

	results := make([]*oraclev1.OracleValue, 0, len(symbols))
	for _, symbol := range symbols {
		task := taskBySymbol[symbol]
		results = append(results, &oraclev1.OracleValue{
			Symbol:        symbol,
			ValueType:     task.GetValueType(),
			Value:         median(valuesBySymbol[symbol]).String(),
			BlockHeight:   height,
			BlockTimeUnix: ctx.BlockTime().Unix(),
		})
	}

	return results, nil
}

func voteExtensionHeight(proposalHeight int64) int64 {
	return proposalHeight - 1
}

func isMinGasPriceOracleSymbol(symbol string) bool {
	return oraclekeeper.NormalizeSymbol(symbol) == oraclekeeper.NormalizeSymbol(appparams.MinGasPriceOracleSymbol)
}

func DecodeVoteExtension(bz []byte) (*oraclev1.OracleVoteExtension, error) {
	extension := &oraclev1.OracleVoteExtension{}
	if err := proto.Unmarshal(bz, extension); err != nil {
		return nil, err
	}
	if err := ValidateVoteExtension(extension); err != nil {
		return nil, err
	}

	return extension, nil
}

func ValidateVoteExtension(extension *oraclev1.OracleVoteExtension) error {
	if extension == nil {
		return fmt.Errorf("oracle vote extension cannot be nil")
	}

	seen := map[string]struct{}{}
	for _, result := range extension.GetResults() {
		symbol := oraclekeeper.NormalizeSymbol(result.GetSymbol())
		if symbol == "" {
			return fmt.Errorf("oracle vote extension result symbol cannot be empty")
		}
		if _, ok := seen[symbol]; ok {
			return fmt.Errorf("duplicate oracle vote extension result for %q", symbol)
		}
		seen[symbol] = struct{}{}

		if result.GetValueType() == oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED {
			return fmt.Errorf("oracle vote extension result value_type cannot be unspecified")
		}
		if result.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
			return fmt.Errorf("oracle vote extension result non-numeric value_type is not supported")
		}
		if result.GetValue() == "" {
			return fmt.Errorf("oracle vote extension result value cannot be empty")
		}
		if result.GetSourceCount() == 0 {
			return fmt.Errorf("oracle vote extension result source_count must be positive")
		}
		if _, err := sdkmath.LegacyNewDecFromStr(result.GetValue()); err != nil {
			return fmt.Errorf("invalid oracle vote extension numeric value: %w", err)
		}
	}

	return nil
}

func median(values []sdkmath.LegacyDec) sdkmath.LegacyDec {
	sort.Slice(values, func(i, j int) bool {
		return values[i].LT(values[j])
	})

	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}

	return values[mid-1].Add(values[mid]).QuoInt64(2)
}

func signedVoteExtensionsFromExtendedCommit(extCommit abcitypes.ExtendedCommitInfo) *oraclev1.OracleSignedVoteExtensions {
	votes := make([]*oraclev1.OracleSignedVoteExtension, 0, len(extCommit.GetVotes()))
	for _, vote := range extCommit.GetVotes() {
		votes = append(votes, &oraclev1.OracleSignedVoteExtension{
			ValidatorAddress:   append([]byte(nil), vote.Validator.Address...),
			ValidatorPower:     vote.Validator.Power,
			BlockIdFlag:        int32(vote.BlockIdFlag),
			VoteExtension:      append([]byte(nil), vote.VoteExtension...),
			ExtensionSignature: append([]byte(nil), vote.ExtensionSignature...),
		})
	}

	return &oraclev1.OracleSignedVoteExtensions{
		Round: extCommit.Round,
		Votes: votes,
	}
}

func extendedCommitFromSignedVoteExtensions(voteExtensions *oraclev1.OracleSignedVoteExtensions) abcitypes.ExtendedCommitInfo {
	if voteExtensions == nil {
		return abcitypes.ExtendedCommitInfo{}
	}

	votes := make([]abcitypes.ExtendedVoteInfo, 0, len(voteExtensions.GetVotes()))
	for _, vote := range voteExtensions.GetVotes() {
		votes = append(votes, abcitypes.ExtendedVoteInfo{
			Validator: abcitypes.Validator{
				Address: append([]byte(nil), vote.GetValidatorAddress()...),
				Power:   vote.GetValidatorPower(),
			},
			BlockIdFlag:        cmtproto.BlockIDFlag(vote.GetBlockIdFlag()),
			VoteExtension:      append([]byte(nil), vote.GetVoteExtension()...),
			ExtensionSignature: append([]byte(nil), vote.GetExtensionSignature()...),
		})
	}

	return abcitypes.ExtendedCommitInfo{
		Round: voteExtensions.GetRound(),
		Votes: votes,
	}
}

func validateExtendedCommitBlockIDFlags(ctx sdk.Context, extCommit abcitypes.ExtendedCommitInfo) error {
	cometInfo := ctx.CometInfo()
	if cometInfo == nil {
		return fmt.Errorf("missing comet info for oracle vote extension validation")
	}
	lastCommit := cometInfo.GetLastCommit()
	if lastCommit == nil {
		return fmt.Errorf("missing last commit info for oracle vote extension validation")
	}
	lastVotes := lastCommit.Votes()
	if lastVotes == nil {
		return fmt.Errorf("missing last commit votes for oracle vote extension validation")
	}
	if len(extCommit.GetVotes()) != lastVotes.Len() {
		return fmt.Errorf("oracle vote extension count %d does not match last commit vote count %d", len(extCommit.GetVotes()), lastVotes.Len())
	}

	for i, vote := range extCommit.GetVotes() {
		expectedFlag := cmtproto.BlockIDFlag(lastVotes.Get(i).GetBlockIDFlag())
		if vote.BlockIdFlag != expectedFlag {
			return fmt.Errorf(
				"oracle vote extension %d block_id_flag %s does not match last commit block_id_flag %s",
				i,
				vote.BlockIdFlag,
				expectedFlag,
			)
		}
	}

	return nil
}

func oracleValuesEqual(a, b []*oraclev1.OracleValue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GetSymbol() != b[i].GetSymbol() ||
			a[i].GetValueType() != b[i].GetValueType() ||
			a[i].GetValue() != b[i].GetValue() ||
			a[i].GetBlockHeight() != b[i].GetBlockHeight() ||
			a[i].GetBlockTimeUnix() != b[i].GetBlockTimeUnix() {
			return false
		}
	}

	return true
}

func containsPayloadAfterFirst(txs [][]byte) bool {
	for i := 1; i < len(txs); i++ {
		if IsProposalTx(txs[i]) {
			return true
		}
	}

	return false
}

func stripPayloadTxs(txs [][]byte) [][]byte {
	stripped := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		if !IsProposalTx(tx) {
			stripped = append(stripped, tx)
		}
	}

	return stripped
}
