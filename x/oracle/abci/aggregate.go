package abci

import (
	"context"
	"fmt"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
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

	if err := a.validateVoteExtensions(ctx, extCommit); err != nil {
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
